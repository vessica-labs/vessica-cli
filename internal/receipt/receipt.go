package receipt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const humanMinutesPerTicket = 45

func Finalize(ctx context.Context, db *state.DB, r *state.Run) (*state.Receipt, error) {
	tickets, _ := db.ListTickets(ctx, r.EpicID)
	closed := 0
	bugs := 0
	for _, t := range tickets {
		if t.Status == "closed" {
			closed++
		}
		if t.Type == "bug" {
			bugs++
		}
	}
	arts, _ := db.ListArtifacts(ctx, r.EpicID, "")
	events, _ := db.ListEvents(ctx, r.ID, 0)
	evidence, _ := db.ListRunEvidence(ctx, r.ID)
	sandboxID := ""
	sandboxExpiresAt := ""
	if sb, err := db.GetSandboxForRun(ctx, r.ID); err == nil {
		sandboxID = sb.ID
		if expires := retention.EffectiveExpiry(sb); !expires.IsZero() {
			sandboxExpiresAt = expires.Format(time.RFC3339Nano)
		}
	}
	var buildEvidence, validationEvidence, mergeEvidence, ticketEvidence []map[string]any
	var infrastructure []map[string]any
	for _, event := range events {
		if event.Type != "run.infrastructure.stage" {
			continue
		}
		var item map[string]any
		if json.Unmarshal([]byte(event.PayloadJSON), &item) == nil {
			item["event_seq"] = event.Seq
			item["recorded_at"] = event.CreatedAt
			infrastructure = append(infrastructure, item)
		}
	}
	for _, ev := range evidence {
		item := map[string]any{
			"id":        ev.ID,
			"phase":     ev.Phase,
			"kind":      ev.Kind,
			"ticket_id": ev.TicketID,
			"status":    ev.Status,
			"body":      jsonBody(ev.BodyJSON),
		}
		switch ev.Phase {
		case "build":
			buildEvidence = append(buildEvidence, item)
		case "validate":
			validationEvidence = append(validationEvidence, item)
		case "code":
			if ev.Kind == "merge" {
				mergeEvidence = append(mergeEvidence, item)
			} else {
				ticketEvidence = append(ticketEvidence, item)
			}
		}
	}
	elapsed := ""
	wallElapsed := ""
	if r.StartedAt != "" && r.FinishedAt != "" {
		if a, err1 := time.Parse(time.RFC3339Nano, r.StartedAt); err1 == nil {
			if b, err2 := time.Parse(time.RFC3339Nano, r.FinishedAt); err2 == nil {
				elapsed = b.Sub(a).String()
			}
		}
	}
	if r.CreatedAt != "" && r.FinishedAt != "" {
		if a, err1 := time.Parse(time.RFC3339Nano, r.CreatedAt); err1 == nil {
			if b, err2 := time.Parse(time.RFC3339Nano, r.FinishedAt); err2 == nil {
				wallElapsed = b.Sub(a).String()
			}
		}
	}
	if elapsed == "" && r.StartedAt != "" {
		if a, err1 := time.Parse(time.RFC3339Nano, r.StartedAt); err1 == nil {
			elapsed = time.Since(a).String()
		}
	}

	body := map[string]any{
		"run_id":             r.ID,
		"epic_id":            r.EpicID,
		"status":             r.Status,
		"preview_url":        r.PreviewURL,
		"pr_url":             r.PRURL,
		"artifact_set_id":    r.ArtifactSetID,
		"artifacts_count":    len(arts),
		"tickets_total":      len(tickets),
		"tickets_closed":     closed,
		"bug_tickets":        bugs,
		"events_count":       len(events),
		"evidence_count":     len(evidence),
		"ticket_evidence":    ticketEvidence,
		"merge_evidence":     mergeEvidence,
		"build":              buildEvidence,
		"validation":         validationEvidence,
		"elapsed":            elapsed,
		"wall_elapsed":       wallElapsed,
		"infrastructure":     infrastructure,
		"runner":             r.Runner,
		"model":              r.Model,
		"reasoning_effort":   r.ReasoningEffort,
		"sandbox":            r.SandboxBackend,
		"sandbox_id":         sandboxID,
		"sandbox_expires_at": sandboxExpiresAt,
		"return_on_intelligence": map[string]any{
			"estimated_human_minutes_avoided": closed * humanMinutesPerTicket,
			"token_runtime_cost_usd":          nil,
			"acceptance_result":               r.Status,
			"quality_eval_result":             "validation_phase_completed",
		},
	}
	var rcpt *state.Receipt
	var err error
	if r.ReceiptID != "" {
		rcpt, err = db.UpdateReceipt(ctx, r.ReceiptID, r.Status, body)
	} else {
		rcpt, err = db.CreateReceipt(ctx, r.ID, r.EpicID, r.Status, body)
	}
	if err != nil {
		return nil, err
	}
	_, _ = db.CreateTrace(ctx, r.ID, fmt.Sprintf("trace for %s", r.ID), map[string]any{
		"run_id": r.ID,
		"phases": state.SoftwareEpicPhases,
		"events": len(events),
	})
	return rcpt, nil
}

func PRBody(ctx context.Context, db *state.DB, r *state.Run) string {
	epic, _ := db.GetEpic(ctx, r.EpicID)
	tickets, _ := db.ListTickets(ctx, r.EpicID)
	arts, _ := db.ListArtifacts(ctx, r.EpicID, "")
	title := ""
	if epic != nil {
		title = epic.Title
	}
	body := fmt.Sprintf("## Summary\n\nVessica run `%s` for epic **%s**.\n\n", r.ID, title)
	body += "### Artifacts\n\n"
	for _, a := range arts {
		body += fmt.Sprintf("- `%s` %s (%s)\n", a.ID, a.Title, a.Type)
	}
	body += "\n### Tickets\n\n"
	for _, t := range tickets {
		body += fmt.Sprintf("- [%s] `%s` %s\n", t.Status, t.ID, t.Title)
	}
	body += "\n### Build And Validation\n\n"
	evidence, _ := db.ListRunEvidence(ctx, r.ID)
	for _, ev := range evidence {
		if ev.Phase == "build" || ev.Phase == "validate" || ev.Kind == "merge" {
			body += fmt.Sprintf("- [%s] `%s` %s", ev.Status, ev.Kind, ev.Phase)
			if ev.TicketID != "" {
				body += fmt.Sprintf(" (%s)", ev.TicketID)
			}
			body += "\n"
		}
	}
	body += "\n### Unresolved Bugs\n\n"
	for _, t := range tickets {
		if t.Type == "bug" && t.Status != "closed" {
			body += fmt.Sprintf("- [%s] `%s` %s", t.Status, t.ID, t.Title)
			if t.TestStep != "" {
				body += fmt.Sprintf(" (%s)", t.TestStep)
			}
			body += "\n"
		}
	}
	body += fmt.Sprintf("\n### Preview\n\n%s\n", r.PreviewURL)
	body += fmt.Sprintf("\n### Receipt\n\n`%s`\n", r.ReceiptID)
	return body
}

func jsonBody(raw string) any {
	var body any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return raw
	}
	return body
}

func ViewJSON(rcpt *state.Receipt) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal([]byte(rcpt.BodyJSON), &body); err != nil {
		return nil, err
	}
	return map[string]any{
		"id":         rcpt.ID,
		"run_id":     rcpt.RunID,
		"epic_id":    rcpt.EpicID,
		"status":     rcpt.Status,
		"created_at": rcpt.CreatedAt,
		"body":       body,
	}, nil
}
