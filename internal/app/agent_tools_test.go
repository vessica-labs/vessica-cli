package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func TestAgentKnowledgeWritesUseRepositoryScope(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	workspace, err := db.EnsureWorkspace(ctx, root, "solo")
	if err != nil {
		t.Fatal(err)
	}
	repository, err := db.EnsureRepository(ctx, workspace.ID, "https://github.com/vessica-labs/vessica-cos.git")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	service := New(db, root, cfg)
	scope, err := service.EnsureRepositoryKnowledgeScope(ctx, repository.ID)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ExecuteAgentTool(ctx, "memory.create", "memory-test", scope.ID, json.RawMessage(`{
		"scope_id":"ws_invalid",
		"type":"decision",
		"title":"Big Rock ranking",
		"content":"Market stature is the first priority.",
		"importance":1,
		"confidence":1,
		"confidence_source":"human_confirmed"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	memory, ok := result.(knowledge.Memory)
	if !ok {
		t.Fatalf("result type=%T, want knowledge.Memory", result)
	}
	if memory.ScopeID != scope.ID {
		t.Fatalf("memory scope=%q, want %q", memory.ScopeID, scope.ID)
	}
	if memory.ConfidenceSource != "human_confirmed" {
		t.Fatalf("confidence source=%q", memory.ConfidenceSource)
	}

	entityResult, err := service.ExecuteAgentTool(ctx, "entity.create", "entity-test", scope.ID, json.RawMessage(`{
		"scope_id":"ws_invalid",
		"type":"organization",
		"display_name":"BCG",
		"aliases":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	entity, ok := entityResult.(knowledge.Entity)
	if !ok {
		t.Fatalf("result type=%T, want knowledge.Entity", entityResult)
	}
	if entity.ScopeID != scope.ID {
		t.Fatalf("entity scope=%q, want %q", entity.ScopeID, scope.ID)
	}
}
