package run

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryTransientStateOperationRecoversPostgresReset(t *testing.T) {
	attempts := 0
	retries := 0
	err := retryTransientStateOperation(context.Background(), "postgres", func() error {
		attempts++
		if attempts < 3 {
			return errors.New("read tcp: connection reset by peer")
		}
		return nil
	}, func(attempt int, delay time.Duration, err error) {
		retries++
		if delay <= 0 || err == nil {
			t.Fatalf("invalid retry callback: attempt=%d delay=%s err=%v", attempt, delay, err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || retries != 2 {
		t.Fatalf("attempts=%d retries=%d", attempts, retries)
	}
}

func TestRetryTransientStateOperationDoesNotRetryPermanentFailure(t *testing.T) {
	attempts := 0
	err := retryTransientStateOperation(context.Background(), "postgres", func() error {
		attempts++
		return errors.New("constraint violation")
	}, nil)
	if err == nil || attempts != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}
