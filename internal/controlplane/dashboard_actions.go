package controlplane

import (
	"context"
	"fmt"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
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
	return s.runLifecycle().Cancel(ctx, runID, "dashboard")
}
func (s *Server) DashboardRetain(ctx context.Context, sandboxID string, duration time.Duration) (any, error) {
	return s.runLifecycle().Retain(ctx, sandboxID, duration)
}
func (s *Server) DashboardDestroy(ctx context.Context, sandboxID string) (any, error) {
	return s.runLifecycle().Destroy(ctx, sandboxID, "dashboard")
}

func (s *Server) runLifecycle() *appservice.RunLifecycle {
	return appservice.NewRunLifecycle(s.DB, ".", s.Config, func(ctx context.Context, sandbox *state.Sandbox, _ string) error {
		if s.Launcher == nil {
			return fmt.Errorf("sandbox launcher is unavailable")
		}
		return s.Launcher.Destroy(ctx, sandbox)
	})
}
