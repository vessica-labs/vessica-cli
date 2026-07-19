package state

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) AppendEvent(ctx context.Context, runID, sandboxID, typ string, payload any) (*Event, error) {
	b, _ := json.Marshal(payload)
	e := &Event{
		ID:          id.New(id.Event),
		RunID:       runID,
		SandboxID:   sandboxID,
		Type:        typ,
		PayloadJSON: string(b),
		CreatedAt:   Now(),
	}
	attempts := 1
	if db.Dialect == "postgres" {
		attempts = 4
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		seq, err := db.appendEventAttempt(ctx, e)
		if err == nil {
			e.Seq = seq
			return e, nil
		}
		lastErr = err
		if db.Dialect != "postgres" {
			break
		}
		// A connection can disappear after Postgres commits but before the
		// client receives the response. Reuse one event ID across attempts and
		// check for that committed row before deciding whether to retry.
		if existing, lookupErr := db.eventByID(ctx, e.ID); lookupErr == nil && existing != nil {
			return existing, nil
		}
		if !retryablePostgresError(err) || attempt == attempts-1 {
			break
		}
		delay := time.Duration(50*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func (db *DB) appendEventAttempt(ctx context.Context, e *Event) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	seq, err := db.nextSequenceTx(ctx, tx, e.RunID)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, db.Rebind(`INSERT INTO events(id, run_id, sandbox_id, seq, type, payload_json, created_at) VALUES (?,?,?,?,?,?,?)`),
		e.ID, nullStr(e.RunID), nullStr(e.SandboxID), seq, e.Type, e.PayloadJSON, e.CreatedAt)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

func (db *DB) eventByID(ctx context.Context, eventID string) (*Event, error) {
	var e Event
	err := db.QueryRow(ctx, `SELECT id, COALESCE(run_id,''), COALESCE(sandbox_id,''), seq, type, payload_json, created_at FROM events WHERE id=?`, eventID).
		Scan(&e.ID, &e.RunID, &e.SandboxID, &e.Seq, &e.Type, &e.PayloadJSON, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func retryablePostgresError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary()) {
		return true
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "40001", "40P01", "08000", "08001", "08003", "08004", "08006", "57P01", "57P02", "57P03":
			return true
		}
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"connection reset by peer",
		"broken pipe",
		"unexpected eof",
		"server closed the connection unexpectedly",
		"connection is closed",
		"conn closed",
		"connection refused",
		"connection timed out",
		"i/o timeout",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
