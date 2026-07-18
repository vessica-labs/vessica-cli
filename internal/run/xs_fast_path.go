package run

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type planningEvidence struct {
	Complexity string         `json:"complexity"`
	Rationale  string         `json:"rationale"`
	Ticket     *plannedTicket `json:"ticket"`
}

// xsTicketPlan reuses the ticket returned by the planning call. This keeps the
// durable ticketize phase and its events while avoiding a second model call for
// work the planner already classified as a single localized change.
func (e *Engine) xsTicketPlan(ctx context.Context, r *state.Run, epic *state.Epic, arts []state.Artifact) ([]plannedTicket, bool, error) {
	evidence, err := e.DB.ListRunEvidence(ctx, r.ID)
	if err != nil {
		return nil, false, err
	}
	var selected planningEvidence
	for i := len(evidence) - 1; i >= 0; i-- {
		if evidence[i].Phase != "plan" || evidence[i].Kind != "planning_bundle" {
			continue
		}
		if json.Unmarshal([]byte(evidence[i].BodyJSON), &selected) == nil {
			break
		}
	}
	if normalizeComplexity(selected.Complexity) != "xs" {
		return nil, false, nil
	}
	ticket := selected.Ticket
	if ticket == nil {
		fallback := deterministicXSTicket(epic, artifactPointers(arts))
		ticket = &fallback
	}
	plan, err := normalizeTicketPlan(ticketPlan{
		Complexity: "xs",
		Rationale:  firstNonEmptyString(selected.Rationale, "deterministic xs fast path"),
		Tickets:    []plannedTicket{*ticket},
	})
	if err != nil {
		return nil, false, err
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "ticketize", "ticket_plan", "", "passed", map[string]any{
		"tickets":    1,
		"complexity": "xs",
		"rationale":  plan.Rationale,
		"model":      "planning_fast_path",
	})
	return plan.Tickets, true, nil
}

func deterministicXSTicket(epic *state.Epic, arts []*state.Artifact) plannedTicket {
	criteria := artifactAcceptanceCriteria(arts)
	if len(criteria) == 0 {
		criteria = []string{
			"The requested outcome is implemented as one localized change.",
			"Engine-managed lint, architecture, build, test, and validation gates pass.",
		}
	}
	return plannedTicket{
		Type:                inferXSTicketType(epic.Title + "\n" + epic.Body),
		Title:               strings.TrimSpace(epic.Title),
		Body:                strings.TrimSpace(epic.Body),
		AcceptanceCriteria:  criteria,
		EstimatedComplexity: "xs",
	}
}

func artifactAcceptanceCriteria(arts []*state.Artifact) []string {
	var criteria []string
	for _, artifact := range arts {
		if artifact == nil || artifact.Type != "test-scenarios" {
			continue
		}
		for _, line := range strings.Split(artifact.Body, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= 3 && line[0] >= '0' && line[0] <= '9' && line[1] == '.' {
				line = strings.TrimSpace(line[2:])
			} else if strings.HasPrefix(line, "- ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
			} else {
				continue
			}
			if line != "" {
				criteria = append(criteria, line)
			}
			if len(criteria) == 5 {
				return criteria
			}
		}
	}
	return criteria
}

func inferXSTicketType(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{"document", "documentation", "readme", "guide", "reference", "copy"} {
		if strings.Contains(lower, marker) {
			return "docs"
		}
	}
	for _, marker := range []string{"fix", "bug", "error", "failure", "broken"} {
		if strings.Contains(lower, marker) {
			return "bug"
		}
	}
	return "feature"
}

func artifactPointers(arts []state.Artifact) []*state.Artifact {
	out := make([]*state.Artifact, 0, len(arts))
	for i := range arts {
		out = append(out, &arts[i])
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
