package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func (db *DB) GetIdempotency(ctx context.Context, key string) (json.RawMessage, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	var raw string
	err := db.QueryRow(ctx, `SELECT result_json FROM idempotency_keys WHERE key=?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(raw), true, nil
}

func (db *DB) PutIdempotency(ctx context.Context, key string, result any) error {
	if key == "" {
		return nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal idempotency result: %w", err)
	}
	if db.Dialect == "postgres" {
		_, err = db.Exec(ctx, `INSERT INTO idempotency_keys(key, result_json, created_at) VALUES (?,?,?) ON CONFLICT (key) DO NOTHING`, key, string(b), Now())
		return err
	}
	_, err = db.Exec(ctx, `INSERT OR IGNORE INTO idempotency_keys(key, result_json, created_at) VALUES (?,?,?)`, key, string(b), Now())
	return err
}
