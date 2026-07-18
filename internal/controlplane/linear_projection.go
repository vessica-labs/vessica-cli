package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (s *Server) SyncRunToLinear(ctx context.Context, runID string) error {
	s.projectionMu.Lock()
	defer s.projectionMu.Unlock()
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	integration, integrationErr := s.DB.GetTrackerIntegration(ctx, "linear")
	if integrationErr != nil && s.Config.Tracker.Provider != "linear" {
		if runTerminalStatus(runRecord.Status) {
			return s.recordTerminalRunKnowledge(ctx, runRecord, "")
		}
		return nil
	}
	if integrationErr != nil {
		return integrationErr
	}
	epicMapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", runRecord.EpicID)
	if err != nil {
		return err
	}
	artifacts, _ := s.DB.ListArtifactsForRun(ctx, runID)
	sortLinearArtifacts(artifacts)
	for _, artifact := range artifacts {
		body := formatLinearArtifactComment(artifact)
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", fmt.Sprintf("linear:artifact:%s:v%d", artifact.ID, artifact.Version), map[string]any{"issue_id": epicMapping.ExternalID, "entity_type": "artifact_comment", "local_id": artifact.ID, "body": body})
	}
	tickets, _ := s.DB.ListTicketsForRun(ctx, runRecord.EpicID, runID)
	for _, ticket := range tickets {
		stateID := s.Config.Tracker.TodoStateID
		if ticket.Status == "claimed" || ticket.Status == "in_progress" {
			stateID = s.Config.Tracker.WIPStateID
		} else if ticket.Status == "closed" {
			stateID = s.Config.Tracker.DoneStateID
		} else if ticket.Status == "blocked" && s.Config.Tracker.BlockedStateID != "" {
			stateID = s.Config.Tracker.BlockedStateID
		}
		key := fmt.Sprintf("linear:ticket:%s:%s", ticket.ID, ticket.Status)
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.subissue", key, map[string]any{"parent_id": epicMapping.ExternalID, "ticket_id": ticket.ID, "title": ticket.Title, "description": ticket.Body, "state_id": stateID})
	}
	events, _ := s.DB.ListEvents(ctx, runID, 0)
	for _, event := range events {
		ticketID, body, ok := formatLinearTicketEventComment(event)
		if !ok {
			continue
		}
		ticketMapping, mappingErr := s.DB.GetExternalMapping(ctx, "linear", "ticket", ticketID)
		if mappingErr != nil {
			continue
		}
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", "linear:ticket-event:"+event.ID, map[string]any{
			"issue_id": ticketMapping.ExternalID, "entity_type": "ticket_event_comment", "local_id": event.ID, "body": body,
		})
	}
	if runRecord.Status == "completed" {
		previewURL := s.projectedPreviewURL(runRecord)
		body := fmt.Sprintf("<!-- vessica:run:%s -->\nVessica completed the run.\n\n- Preview: %s\n- Draft PR: %s\n- Receipt: `%s`", runID, previewURL, runRecord.PRURL, runRecord.ReceiptID)
		acceptURL, rollbackURL := s.reviewURL(runID, "approve"), s.reviewURL(runID, "rollback")
		if acceptURL != "" && rollbackURL != "" && runRecord.PRMode != "merged" && runRecord.PRMode != "rolled_back" {
			body += fmt.Sprintf("\n\n**[Accept and Merge](%s)** · [Rollback](%s)", acceptURL, rollbackURL)
		}
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", completionProjectionKey(runRecord), map[string]any{"issue_id": epicMapping.ExternalID, "entity_type": "run_comment", "local_id": runID, "body": body})
	}
	if runTerminalStatus(runRecord.Status) {
		return s.recordTerminalRunKnowledge(ctx, runRecord, epicMapping.ExternalID)
	}
	return nil
}

func completionProjectionKey(runRecord *state.Run) string {
	digest := sha256.Sum256([]byte(runRecord.PreviewURL))
	return fmt.Sprintf("linear:run:completed:v5:%s:%x", runRecord.ID, digest[:8])
}

// projectedPreviewURL returns only a preview that was externally healthchecked
// and persisted by the control plane. Never synthesize a success-shaped URL.
func (s *Server) projectedPreviewURL(runRecord *state.Run) string {
	if runRecord == nil || !runRecord.Preview {
		return ""
	}
	if runRecord.SandboxBackend == "railway" {
		parsed, err := url.Parse(runRecord.PreviewURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Query().Get("cap") == "" {
			return ""
		}
	}
	return runRecord.PreviewURL
}

func linearArtifactRank(typ string) int {
	switch typ {
	case "prd":
		return 0
	case "adr":
		return 1
	case "design-spec":
		return 2
	case "test-scenarios":
		return 3
	default:
		return 4
	}
}

func sortLinearArtifacts(artifacts []state.Artifact) {
	sort.SliceStable(artifacts, func(i, j int) bool {
		return linearArtifactRank(artifacts[i].Type) < linearArtifactRank(artifacts[j].Type)
	})
}

func formatLinearArtifactComment(artifact state.Artifact) string {
	body := strings.TrimSpace(artifact.Body)
	marker := fmt.Sprintf("<!-- vessica:artifact:%s:v%d -->", artifact.ID, artifact.Version)
	if body == "" {
		return marker
	}
	return body + "\n\n" + marker
}

func formatLinearTicketEventComment(event state.Event) (string, string, bool) {
	var payload struct {
		TicketID string   `json:"ticket_id"`
		Message  string   `json:"message"`
		Error    string   `json:"error"`
		Commit   string   `json:"commit"`
		Files    []string `json:"files"`
	}
	if json.Unmarshal([]byte(event.PayloadJSON), &payload) != nil || strings.TrimSpace(payload.TicketID) == "" {
		return "", "", false
	}
	var title, detail string
	switch event.Type {
	case "ticket.claimed":
		title = "Coding started"
		detail = "A coding agent started work on this ticket."
	case "agent.output":
		title = "Agent summary"
		detail = strings.TrimSpace(payload.Message)
		if detail == "" {
			return "", "", false
		}
	case "ticket.closed":
		title = "Coding completed"
		parts := []string{"The coding work completed successfully."}
		if payload.Commit != "" {
			parts = append(parts, "Commit: `"+payload.Commit+"`")
		}
		if len(payload.Files) > 0 {
			parts = append(parts, "Changed files: "+strings.Join(payload.Files, ", "))
		}
		detail = strings.Join(parts, "\n\n")
	case "ticket.failed":
		title = "Coding error"
		detail = strings.TrimSpace(redaction.Redact(payload.Error))
		if detail == "" {
			detail = "The coding agent could not complete this ticket."
		}
	default:
		return "", "", false
	}
	if len(detail) > 6000 {
		detail = detail[:6000] + "..."
	}
	marker := "<!-- vessica:ticket-event:" + event.ID + " -->"
	return payload.TicketID, "**" + title + "**\n\n" + detail + "\n\n" + marker, true
}

func (s *Server) completeLinearParentIfAllChildrenDone(ctx context.Context, issueID string) error {
	if s.Linear == nil || strings.TrimSpace(s.Config.Tracker.DoneStateID) == "" || strings.TrimSpace(issueID) == "" {
		return nil
	}
	issue, err := s.Linear.GetIssue(ctx, issueID)
	if err != nil {
		return err
	}
	if issue.Parent == nil || strings.TrimSpace(issue.Parent.ID) == "" {
		return nil
	}
	parent, err := s.Linear.GetIssue(ctx, issue.Parent.ID)
	if err != nil {
		return err
	}
	if len(parent.Children.Nodes) == 0 {
		return nil
	}
	for _, child := range parent.Children.Nodes {
		if child.State.ID != s.Config.Tracker.DoneStateID {
			return nil
		}
	}
	if parent.State.ID == s.Config.Tracker.DoneStateID {
		return nil
	}
	return s.Linear.UpdateIssueState(ctx, parent.ID, s.Config.Tracker.DoneStateID)
}
