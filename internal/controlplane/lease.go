package controlplane

import (
	"context"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func monitorControlPlaneLease(ctx context.Context, lease *state.ControlPlaneLease, errors chan<- error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := lease.Heartbeat(ctx); err != nil {
				select {
				case errors <- err:
				default:
				}
				return
			}
		}
	}
}
