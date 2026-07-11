package state

import (
	"context"
	"database/sql"
	"fmt"
)

func (db *DB) PutHostedCredential(ctx context.Context, provider, encryptedJSON string) error {
	if provider == "" || encryptedJSON == "" {
		return fmt.Errorf("provider and encrypted credential are required")
	}
	now := Now()
	_, err := db.Exec(ctx, `INSERT INTO hosted_credentials(provider, encrypted_json, created_at, updated_at)
		VALUES (?,?,?,?) ON CONFLICT(provider) DO UPDATE SET encrypted_json=excluded.encrypted_json, updated_at=excluded.updated_at`, provider, encryptedJSON, now, now)
	return err
}

func (db *DB) GetHostedCredential(ctx context.Context, provider string) (string, error) {
	var encrypted string
	err := db.QueryRow(ctx, `SELECT encrypted_json FROM hosted_credentials WHERE provider=?`, provider).Scan(&encrypted)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("hosted credential not found: %s", provider)
	}
	return encrypted, err
}

func (db *DB) DeleteHostedCredential(ctx context.Context, provider string) error {
	_, err := db.Exec(ctx, `DELETE FROM hosted_credentials WHERE provider=?`, provider)
	return err
}
