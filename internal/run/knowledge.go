package run

import (
	"context"
	"fmt"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func (e *Engine) recordWorkflowKnowledge(ctx context.Context, r *state.Run, eventType, summary, key string, refs ...knowledge.ExternalRef) {
	g, err := knowledgegateway.Open(e.Root, e.Config, e.workspaceID(ctx))
	if err != nil {
		return
	}
	defer g.Close()
	scope, err := g.EnsureRepositoryScope(ctx, knowledgegateway.CanonicalRepository(e.Config.Repo.Remote, e.Root), e.Config.Repo.Remote)
	if err != nil {
		return
	}
	base := []knowledge.ExternalRef{{System: "vessica.epic", ID: r.EpicID}, {System: "vessica.run", ID: r.ID}}
	if epic, epicErr := e.DB.GetEpic(ctx, r.EpicID); epicErr == nil && epic.ExternalID != "" {
		base = append(base, knowledge.ExternalRef{System: "linear.issue", ID: epic.ExternalID})
	}
	if tickets, ticketErr := e.DB.ListTickets(ctx, r.EpicID); ticketErr == nil {
		for _, ticket := range tickets {
			base = append(base, knowledge.ExternalRef{System: "vessica.ticket", ID: ticket.ID})
			if ticket.ExternalID != "" {
				base = append(base, knowledge.ExternalRef{System: "linear.issue", ID: ticket.ExternalID})
			}
		}
	}
	if artifacts, artifactErr := e.DB.ListArtifacts(ctx, r.EpicID, ""); artifactErr == nil {
		for _, artifact := range artifacts {
			base = append(base, knowledge.ExternalRef{System: "vessica.artifact", ID: artifact.ID})
		}
	}
	if r.TicketID != "" {
		base = append(base, knowledge.ExternalRef{System: "vessica.ticket", ID: r.TicketID})
	}
	if r.ReceiptID != "" {
		base = append(base, knowledge.ExternalRef{System: "vessica.receipt", ID: r.ReceiptID})
	}
	if r.PRURL != "" {
		base = append(base, knowledge.ExternalRef{System: "github.pull_request", ID: r.PRURL, URL: r.PRURL})
	}
	base = append(base, refs...)
	w := knowledge.WorkflowEvent{ID: key, RepositoryScopeID: scope.ID, Type: eventType, Summary: summary, OccurredAt: time.Now().UTC(), Actor: knowledge.Actor{ID: "ves-run-engine", Type: "service"}, References: base, Metadata: map[string]any{"run_status": r.Status, "phase": r.CurrentPhase}}
	if e.Config.Knowledge.Mode == "hosted" {
		if integration, integrationErr := e.DB.GetTrackerIntegration(ctx, "linear"); integrationErr == nil {
			_, _ = e.DB.EnqueueOutbox(ctx, integration.ID, "knowledge.workflow_event", "knowledge:"+key, w)
			return
		}
	}
	_, err = g.Workflow(ctx, key, w)
	_ = err
}

func (e *Engine) mirrorArtifactKnowledge(ctx context.Context, r *state.Run, a *state.Artifact) {
	if a == nil {
		return
	}
	g, err := knowledgegateway.Open(e.Root, e.Config, e.workspaceID(ctx))
	if err != nil {
		return
	}
	defer g.Close()
	scope, err := g.EnsureRepositoryScope(ctx, knowledgegateway.CanonicalRepository(e.Config.Repo.Remote, e.Root), e.Config.Repo.Remote)
	if err != nil {
		return
	}
	v := knowledge.Artifact{ID: a.ID, ScopeID: scope.ID, Type: a.Type, Title: a.Title, Status: "active", Content: a.Body, SourceRef: &knowledge.ExternalRef{System: "vessica.run", ID: r.ID}, Metadata: map[string]any{"epic_id": r.EpicID, "source_run_id": r.ID}}
	if e.Config.Knowledge.Mode == "hosted" {
		if integration, integrationErr := e.DB.GetTrackerIntegration(ctx, "linear"); integrationErr == nil {
			_, _ = e.DB.EnqueueOutbox(ctx, integration.ID, "knowledge.artifact", "knowledge:artifact:"+a.ID, v)
			return
		}
	}
	_, err = g.CreateArtifact(ctx, "run-artifact:"+a.ID, v)
	if err != nil {
		e.emit(ctx, r.ID, "warning", map[string]any{"message": "knowledge artifact mirror failed: " + err.Error(), "artifact_id": a.ID})
	}
}

func (e *Engine) workspaceID(ctx context.Context) string {
	if e.Config.Knowledge.WorkspaceID != "" {
		return e.Config.Knowledge.WorkspaceID
	}
	if ws, err := e.DB.GetWorkspace(ctx); err == nil {
		return ws.ID
	}
	return fmt.Sprintf("local-%x", []byte(e.Root))
}

// RecordRunKnowledge exposes durable terminal/refinement knowledge production
// to CLI lifecycle commands that do not execute through Engine.execute.
func (e *Engine) RecordRunKnowledge(ctx context.Context, r *state.Run, eventType, summary, key string) {
	e.recordWorkflowKnowledge(ctx, r, eventType, summary, key)
}
