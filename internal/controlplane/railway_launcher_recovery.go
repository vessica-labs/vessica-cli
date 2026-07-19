package controlplane

import (
	"context"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func launchQueueBaseline(runRecord *state.Run) time.Time {
	if runRecord == nil {
		return time.Time{}
	}
	created, _ := time.Parse(time.RFC3339Nano, runRecord.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, runRecord.UpdatedAt)
	if updated.After(created) {
		return updated
	}
	return created
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (l *RailwayLauncher) waitForRunTerminalAfterDisconnect(ctx context.Context, runID string, timeout time.Duration) (*state.Run, error) {
	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		runRecord, err := l.DB.GetRun(waitCtx, runID)
		if err != nil {
			return nil, err
		}
		switch runRecord.Status {
		case "completed", "failed", "cancelled":
			return runRecord, nil
		}
		select {
		case <-waitCtx.Done():
			return runRecord, waitCtx.Err()
		case <-ticker.C:
		}
	}
}
