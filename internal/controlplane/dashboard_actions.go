package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/retention"
)

func (s *Server) DashboardRefine(ctx context.Context, runID, prompt string) (any, error) {
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if runRecord.Status != "completed" || runRecord.PRMode == "merged" || runRecord.PRMode == "rolled_back" {
		return nil, fmt.Errorf("run is not available for refinement")
	}
	prompter, ok := s.Launcher.(hostedRunPrompter)
	if !ok {
		return nil, fmt.Errorf("control plane cannot prompt retained sandboxes")
	}
	requestID := id.New("review")
	comment := fmt.Sprintf("<!-- vessica:review-request:%s -->\n**Revision requested from the Vessica dashboard**\n\n%s\n\nRun: `%s`", requestID, prompt, runID)
	if err = s.enqueueLinearReviewComment(ctx, runRecord, "review_request", requestID, "linear:review:request:"+requestID, comment); err != nil {
		return nil, err
	}
	return prompter.Prompt(ctx, runRecord, prompt)
}
func (s *Server) DashboardApprove(ctx context.Context, runID string) (any, error) {
	return s.approveRun(ctx, runID)
}
func (s *Server) DashboardRollback(ctx context.Context, runID string) (any, error) {
	if err := s.rollbackRun(ctx, runID); err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "rolled_back": true}, nil
}
func (s *Server) DashboardCancel(ctx context.Context, runID string) (any, error) {
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if runRecord.Status == "completed" || runRecord.Status == "failed" || runRecord.Status == "cancelled" {
		return nil, fmt.Errorf("run cannot be cancelled from status %s", runRecord.Status)
	}
	runRecord.Status = "cancelled"
	runRecord.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = s.DB.UpdateRun(ctx, runRecord); err != nil {
		return nil, err
	}
	sandboxes, _ := s.DB.ListSandboxesForRun(ctx, runID)
	for i := range sandboxes {
		if s.Launcher != nil {
			_ = s.Launcher.Destroy(ctx, &sandboxes[i])
		}
	}
	_, _ = s.DB.AppendEvent(ctx, runID, "", "run.cancelled", map[string]any{"source": "dashboard"})
	return runRecord, nil
}
func (s *Server) DashboardRetain(ctx context.Context, sandboxID string, duration time.Duration) (any, error) {
	record, err := s.DB.GetSandbox(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(record.Status, "destroyed") || strings.EqualFold(record.Status, "expired") {
		return nil, fmt.Errorf("sandbox is unavailable")
	}
	if err = retention.Retain(ctx, s.DB, record, duration); err != nil {
		return nil, err
	}
	return record, nil
}
func (s *Server) DashboardDestroy(ctx context.Context, sandboxID string) (any, error) {
	record, err := s.DB.GetSandbox(ctx, sandboxID)
	if err != nil {
		return nil, err
	}
	if s.Launcher == nil {
		return nil, fmt.Errorf("sandbox launcher is unavailable")
	}
	if err = s.Launcher.Destroy(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}
