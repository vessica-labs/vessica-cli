package tracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type Status struct {
	Provider  string `json:"provider"`
	Mode      string `json:"mode"`
	Connected bool   `json:"connected"`
	Message   string `json:"message"`
	Mappings  int    `json:"mappings,omitempty"`
}

func Connect(provider string) (*Status, error) {
	provider = strings.ToLower(provider)
	switch provider {
	case "linear", "jira":
		return &Status{Provider: provider, Mode: "best_efforts", Connected: true, Message: "connected; Vessica remains source of truth and will mirror mappings/status"}, nil
	default:
		return nil, fmt.Errorf("unsupported tracker: %s", provider)
	}
}

func Sync(ctx context.Context, db *state.DB, provider string) (map[string]any, error) {
	provider = normalizeProvider(provider)
	epics, err := db.ListEpics(ctx)
	if err != nil {
		return nil, err
	}
	var pushed int
	var ids []string
	for _, e := range epics {
		m, err := Push(ctx, db, provider, "epic", e.ID)
		if err != nil {
			return nil, err
		}
		pushed++
		ids = append(ids, m.ExternalID)
	}
	return map[string]any{
		"provider":     provider,
		"mode":         "best_efforts",
		"pushed":       pushed,
		"pulled":       0,
		"external_ids": ids,
		"message":      "mirrored local Vessica records to tracker mapping table; pull is unsupported in v1",
	}, nil
}

func Push(ctx context.Context, db *state.DB, provider, entityType, localID string) (*state.ExternalMapping, error) {
	provider = normalizeProvider(provider)
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	now := state.Now()
	externalID := fmt.Sprintf("%s-ves-%s", provider, localID)
	meta := map[string]any{
		"mode":      "best_efforts",
		"source":    "vessica",
		"pushed_at": now,
		"status":    "mirrored",
	}
	metaJSON, _ := json.Marshal(meta)
	m := &state.ExternalMapping{
		ID:          id.New("map"),
		WorkspaceID: ws.ID,
		Provider:    provider,
		EntityType:  entityType,
		LocalID:     localID,
		ExternalID:  externalID,
		MetaJSON:    string(metaJSON),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if db.Dialect == "postgres" {
		_, err = db.Exec(ctx, `INSERT INTO external_mappings(id, workspace_id, provider, entity_type, local_id, external_id, meta_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT (provider, entity_type, local_id) DO UPDATE SET external_id=EXCLUDED.external_id, meta_json=EXCLUDED.meta_json, updated_at=EXCLUDED.updated_at`,
			m.ID, m.WorkspaceID, m.Provider, m.EntityType, m.LocalID, m.ExternalID, m.MetaJSON, m.CreatedAt, m.UpdatedAt)
	} else {
		_, err = db.Exec(ctx, `INSERT INTO external_mappings(id, workspace_id, provider, entity_type, local_id, external_id, meta_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(provider, entity_type, local_id) DO UPDATE SET external_id=excluded.external_id, meta_json=excluded.meta_json, updated_at=excluded.updated_at`,
			m.ID, m.WorkspaceID, m.Provider, m.EntityType, m.LocalID, m.ExternalID, m.MetaJSON, m.CreatedAt, m.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}
	return getMapping(ctx, db, provider, entityType, localID)
}

func GetStatus(ctx context.Context, db *state.DB, provider string) *Status {
	provider = normalizeProvider(provider)
	status := &Status{Provider: provider, Mode: "best_efforts", Connected: provider == "linear" || provider == "jira", Message: "connected; mappings are local best-efforts mirrors"}
	if db != nil {
		var n int
		_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM external_mappings WHERE provider=?`, provider).Scan(&n)
		status.Mappings = n
	}
	return status
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || provider == "none" {
		return "linear"
	}
	return provider
}

func getMapping(ctx context.Context, db *state.DB, provider, entityType, localID string) (*state.ExternalMapping, error) {
	var m state.ExternalMapping
	err := db.QueryRow(ctx, `SELECT id, workspace_id, provider, entity_type, local_id, external_id, meta_json, created_at, updated_at FROM external_mappings WHERE provider=? AND entity_type=? AND local_id=?`,
		provider, entityType, localID).
		Scan(&m.ID, &m.WorkspaceID, &m.Provider, &m.EntityType, &m.LocalID, &m.ExternalID, &m.MetaJSON, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mapping not found")
	}
	return &m, err
}
