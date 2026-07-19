package state

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryablePostgresError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "bad connection", err: driver.ErrBadConn, want: true},
		{name: "wrapped reset", err: fmt.Errorf("allocate sequence: read tcp: %w", errors.New("connection reset by peer")), want: true},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "cancelled", err: context.Canceled, want: false},
		{name: "validation", err: errors.New("duplicate key violates unique constraint"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryablePostgresError(test.err); got != test.want {
				t.Fatalf("retryablePostgresError(%v)=%v want %v", test.err, got, test.want)
			}
		})
	}
}
