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
	if scope.CanonicalKey != "repo:https://github.com/vessica-labs/vessica-cos" {
		t.Fatalf("scope canonical key=%q", scope.CanonicalKey)
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
	duplicateResult, err := service.ExecuteAgentTool(ctx, "entity.create", "entity-duplicate", scope.ID, json.RawMessage(`{
		"type":"organization",
		"display_name":"Boston Consulting Group",
		"aliases":["BCG"]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := duplicateResult.(knowledge.Entity)
	previewArgs, _ := json.Marshal(map[string]any{
		"source_entity_id": duplicate.ID,
		"target_entity_id": entity.ID,
		"dry_run":          true,
	})
	previewResult, err := service.ExecuteAgentTool(ctx, "entity.merge", "entity-merge-preview", scope.ID, previewArgs)
	if err != nil {
		t.Fatal(err)
	}
	preview := previewResult.(knowledge.EntityMergeResult)
	if preview.Applied || !preview.DryRun {
		t.Fatalf("merge preview=%#v", preview)
	}
	if _, err := service.ExecuteAgentTool(ctx, "entity.merge", "entity-merge-unconfirmed", scope.ID, json.RawMessage(`{
		"source_entity_id":"source",
		"target_entity_id":"target"
	}`)); err == nil {
		t.Fatal("unconfirmed agent merge should fail")
	}
	mergeArgs, _ := json.Marshal(map[string]any{
		"source_entity_id": duplicate.ID,
		"target_entity_id": entity.ID,
		"confirm":          true,
	})
	mergeResult, err := service.ExecuteAgentTool(ctx, "entity.merge", "entity-merge", scope.ID, mergeArgs)
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeResult.(knowledge.EntityMergeResult)
	if !merged.Applied || merged.Source.State != "archived" {
		t.Fatalf("merge result=%#v", merged)
	}
}
