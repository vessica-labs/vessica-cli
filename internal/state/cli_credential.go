package state

import (
	"context"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

func (db *DB) CreateCLICredential(ctx context.Context, subject, tokenHash string) error {
	workspace, err := db.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO cli_credentials(id,workspace_id,subject,token_hash,created_at) VALUES(?,?,?,?,?)`, id.New("cred"), workspace.ID, strings.TrimSpace(subject), tokenHash, Now())
	return err
}

func (db *DB) HasCLICredential(ctx context.Context, tokenHash string) bool {
	var id string
	err := db.QueryRow(ctx, `SELECT id FROM cli_credentials WHERE token_hash=? AND revoked_at IS NULL`, tokenHash).Scan(&id)
	return err == nil
}
