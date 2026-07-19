package run

import (
	"context"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (e *Engine) retryTicketizeState(ctx context.Context, r *state.Run, operation string, fn func() error) error {
	return retryTransientStateOperation(ctx, e.DB.Dialect, fn, func(attempt int, delay time.Duration, err error) {
		e.emit(ctx, r.ID, "state.retrying", map[string]any{
			"phase":      "ticketize",
			"operation":  operation,
			"attempt":    attempt + 1,
			"delay_ms":   delay.Milliseconds(),
			"last_error": redaction.Redact(err.Error()),
		})
	})
}

func retryTransientStateOperation(ctx context.Context, dialect string, fn func() error, onRetry func(int, time.Duration, error)) error {
	attempts := 1
	if dialect == "postgres" {
		attempts = 4
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt == attempts-1 || !state.IsRetryablePostgresError(lastErr) {
			break
		}
		delay := time.Duration(50*(1<<attempt)) * time.Millisecond
		if onRetry != nil {
			onRetry(attempt, delay, lastErr)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}
