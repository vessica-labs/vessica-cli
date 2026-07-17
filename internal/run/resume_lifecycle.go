package run

import (
	"context"
	"errors"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func resumeStartPhase(start string, explicit bool, phases []state.RunPhase) string {
	if start == "" || explicit {
		return start
	}
	for _, phase := range phases {
		if phase.Phase == start && (phase.Status == "completed" || phase.Status == "skipped") {
			if index := phaseIndex(start); index >= 0 && index+1 < len(state.SoftwareEpicPhases) {
				return state.SoftwareEpicPhases[index+1]
			}
			break
		}
	}
	return start
}

func (e *Engine) cancelledRun(ctx context.Context, runID string, phaseErr error) (*state.Run, error) {
	stableContext := context.WithoutCancel(ctx)
	current, err := e.DB.GetRun(stableContext, runID)
	if err != nil {
		return nil, err
	}
	if current.Status != "cancelled" && !errors.Is(phaseErr, context.Canceled) {
		return nil, nil
	}
	if current.Status != "cancelled" {
		current.Status = "cancelled"
		current.Error = ""
		current.FinishedAt = state.Now()
		if err := e.DB.UpdateRun(stableContext, current); err != nil {
			return nil, err
		}
	}
	_ = e.DB.SetPhaseStatus(stableContext, runID, current.CurrentPhase, "failed", "cancelled")
	return current, nil
}
