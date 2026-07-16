package state

import (
	"context"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

type EpicSpec struct {
	Title   string       `json:"title"`
	Body    string       `json:"body"`
	Tickets []TicketSpec `json:"tickets,omitempty"`
}

type TicketSpec struct {
	Key       string   `json:"key,omitempty"`
	Type      string   `json:"type,omitempty"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type CreatedEpicSpec struct {
	Epic    *Epic     `json:"epic"`
	Tickets []*Ticket `json:"tickets"`
}

func ValidateEpicSpec(spec EpicSpec) error {
	if strings.TrimSpace(spec.Title) == "" {
		return fmt.Errorf("epic title is required")
	}
	keys := map[string]bool{}
	for i, ticket := range spec.Tickets {
		if strings.TrimSpace(ticket.Title) == "" {
			return fmt.Errorf("ticket %d title is required", i+1)
		}
		key := strings.TrimSpace(ticket.Key)
		if key == "" {
			key = fmt.Sprintf("ticket-%d", i+1)
		}
		if keys[key] {
			return fmt.Errorf("duplicate ticket key %q", key)
		}
		keys[key] = true
	}
	for i, ticket := range spec.Tickets {
		for _, dep := range ticket.DependsOn {
			if !keys[dep] {
				return fmt.Errorf("ticket %d depends on unknown key %q", i+1, dep)
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	deps := map[string][]string{}
	for i, ticket := range spec.Tickets {
		key := ticket.Key
		if key == "" {
			key = fmt.Sprintf("ticket-%d", i+1)
		}
		deps[key] = ticket.DependsOn
	}
	var visit func(string) error
	visit = func(key string) error {
		if visiting[key] {
			return fmt.Errorf("ticket dependency cycle includes %q", key)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		for _, dep := range deps[key] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[key] = false
		visited[key] = true
		return nil
	}
	for key := range deps {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) CreateEpicFromSpec(ctx context.Context, spec EpicSpec) (*CreatedEpicSpec, error) {
	repository, err := db.GetRepository(ctx, "")
	if err != nil {
		return nil, err
	}
	return db.CreateEpicFromSpecForRepository(ctx, repository.ID, spec)
}

func (db *DB) CreateEpicFromSpecForRepository(ctx context.Context, repositoryID string, spec EpicSpec) (*CreatedEpicSpec, error) {
	if err := ValidateEpicSpec(spec); err != nil {
		return nil, err
	}
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	repository, err := db.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repository.WorkspaceID != ws.ID {
		return nil, fmt.Errorf("repository %s does not belong to workspace %s", repository.ID, ws.ID)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := Now()
	epic := &Epic{ID: id.New(id.Epic), WorkspaceID: ws.ID, RepositoryID: repository.ID, Title: strings.TrimSpace(spec.Title), Body: spec.Body, Status: "draft", CreatedAt: now, UpdatedAt: now}
	q := db.Rebind(`INSERT INTO epics(id, workspace_id, repository_id, title, body, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`)
	if _, err = tx.ExecContext(ctx, q, epic.ID, epic.WorkspaceID, epic.RepositoryID, epic.Title, epic.Body, epic.Status, epic.CreatedAt, epic.UpdatedAt); err != nil {
		return nil, err
	}
	ids := map[string]string{}
	created := make([]*Ticket, 0, len(spec.Tickets))
	for i, input := range spec.Tickets {
		key := input.Key
		if key == "" {
			key = fmt.Sprintf("ticket-%d", i+1)
		}
		typ := input.Type
		if typ == "" {
			typ = "feature"
		}
		ticket := &Ticket{ID: id.New(id.Ticket), WorkspaceID: ws.ID, EpicID: epic.ID, Type: typ, Title: strings.TrimSpace(input.Title), Body: input.Body, Status: "ready", CreatedAt: now, UpdatedAt: now}
		q = db.Rebind(`INSERT INTO tickets(id, workspace_id, epic_id, source_run_id, type, title, body, status, discovered_from_run_id, test_step, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
		if _, err = tx.ExecContext(ctx, q, ticket.ID, ticket.WorkspaceID, ticket.EpicID, nil, ticket.Type, ticket.Title, ticket.Body, ticket.Status, nil, nil, ticket.CreatedAt, ticket.UpdatedAt); err != nil {
			return nil, err
		}
		ids[key] = ticket.ID
		created = append(created, ticket)
	}
	for i, input := range spec.Tickets {
		for _, dep := range input.DependsOn {
			q = db.Rebind(`INSERT INTO ticket_dependencies(ticket_id, depends_on) VALUES (?,?)`)
			if _, err = tx.ExecContext(ctx, q, created[i].ID, ids[dep]); err != nil {
				return nil, err
			}
			created[i].DependsOn = append(created[i].DependsOn, ids[dep])
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &CreatedEpicSpec{Epic: epic, Tickets: created}, nil
}
