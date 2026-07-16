package onboarding

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var Stages = []string{"repository_preflight", "provider_authentication", "workspace_discovery", "sandbox_entitlement", "plan_ready", "resource_provision", "service_deploy", "knowledge_ready", "owner_claim", "repository_attach", "runner_checkpoint", "repository_scan", "harness_synthesis", "harness_apply", "repository_mapping", "hosted_verification", "completed"}

type Stage struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}
type Operation struct {
	ID           string         `json:"id"`
	Repository   string         `json:"repository"`
	Status       string         `json:"status"`
	CurrentStage string         `json:"current_stage"`
	Stages       []Stage        `json:"stages"`
	Result       map[string]any `json:"result,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	Error        string         `json:"error,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

func New(id, repository string) *Operation {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	op := &Operation{ID: id, Repository: repository, Status: "pending", CreatedAt: now, UpdatedAt: now, Result: map[string]any{}}
	for _, name := range Stages {
		op.Stages = append(op.Stages, Stage{Name: name, Status: "pending"})
	}
	return op
}
func (o *Operation) Set(name, status, message string) {
	o.CurrentStage = name
	o.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if status == "failed" {
		o.Status = "failed"
	} else if name == "completed" && status == "succeeded" {
		o.Status = "completed"
	}
	for i := range o.Stages {
		if o.Stages[i].Name == name {
			o.Stages[i].Status = status
			o.Stages[i].Message = message
			o.Stages[i].UpdatedAt = o.UpdatedAt
			return
		}
	}
}
func RegistryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vessica", "client.db"), nil
}

func openClientDB() (*sql.DB, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS onboarding_operations(id TEXT PRIMARY KEY, repository TEXT NOT NULL, document_json TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS installations(project_id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, endpoint TEXT NOT NULL UNIQUE, credential_ref TEXT NOT NULL, config_json TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return db, nil
}

func Load() ([]Operation, error) {
	db, err := openClientDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT document_json FROM onboarding_operations ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var operations []Operation
	for rows.Next() {
		var document string
		if err := rows.Scan(&document); err != nil {
			return nil, err
		}
		var operation Operation
		if err := json.Unmarshal([]byte(document), &operation); err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}
func Save(op *Operation) error {
	db, err := openClientDB()
	if err != nil {
		return err
	}
	defer db.Close()
	body, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO onboarding_operations(id,repository,document_json,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET repository=excluded.repository,document_json=excluded.document_json,updated_at=excluded.updated_at`, op.ID, op.Repository, string(body), op.UpdatedAt)
	return err
}
func Find(id, repository string) (*Operation, error) {
	ops, err := Load()
	if err != nil {
		return nil, err
	}
	for i := len(ops) - 1; i >= 0; i-- {
		if (id != "" && ops[i].ID == id) || (id == "" && ops[i].Repository == repository) {
			return &ops[i], nil
		}
	}
	return nil, os.ErrNotExist
}
