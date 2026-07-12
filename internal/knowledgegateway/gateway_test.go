package knowledgegateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	knowledgeclient "github.com/vessica-labs/vessica-knowledge-server/client"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
	knowledgeserver "github.com/vessica-labs/vessica-knowledge-server/server"
)

func TestLocalAndHostedReadContractsMatch(t *testing.T) {
	ctx := context.Background()
	workspace := "kwsp_parity"
	localStore, err := ks.OpenSQLite(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	local := &Gateway{mode: "local", workspace: workspace, store: localStore, local: ks.NewService(localStore, nil)}
	scope, err := local.EnsureRepositoryScope(ctx, "github.com/vessica/demo", "Demo")
	if err != nil {
		t.Fatal(err)
	}
	entity, err := local.CreateEntity(ctx, "entity", ks.Entity{ScopeID: scope.ID, Type: "repository", DisplayName: "Dashboard repository", Aliases: []string{"dashboard"}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := local.CreateArtifact(ctx, "artifact", ks.Artifact{ScopeID: scope.ID, Type: "adr", Title: "Dashboard ADR", Content: "Embedded dashboard contract"})
	if err != nil {
		t.Fatal(err)
	}
	artifact.Content = "Embedded dashboard contract v2"
	if _, err = local.VersionArtifact(ctx, "artifact-v2", artifact); err != nil {
		t.Fatal(err)
	}
	memory, err := local.CreateMemory(ctx, "memory", ks.Memory{ScopeID: scope.ID, Type: "fact", Title: "Dashboard fact", Content: "The dashboard is embedded", Importance: 0.7, Confidence: 0.9, ConfidenceSource: "human_confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	memory.Content = "The dashboard is securely embedded"
	if _, err = local.VersionMemory(ctx, "memory-v2", memory); err != nil {
		t.Fatal(err)
	}
	if _, err = local.CreateRelationship(ctx, "relationship", ks.Relationship{ScopeID: scope.ID, FromType: "entity", FromID: entity.ID, Predicate: "documented_by", ToType: "artifact", ToID: artifact.ID, Confidence: 1}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := local.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}

	hostedStore, err := ks.OpenSQLite(filepath.Join(t.TempDir(), "hosted.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer hostedStore.Close()
	if err = hostedStore.Import(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer((&knowledgeserver.Server{Service: ks.NewService(hostedStore, nil), Token: "test-token", DefaultWorkspace: workspace}).Handler())
	defer httpServer.Close()
	hosted := &Gateway{mode: "hosted", workspace: workspace, remote: &knowledgeclient.Client{BaseURL: httpServer.URL, Token: "test-token", Actor: ks.Actor{ID: "test", Type: "user"}}}

	assertSame := func(name string, localValue, hostedValue any, localErr, hostedErr error) {
		t.Helper()
		if localErr != nil || hostedErr != nil {
			t.Fatalf("%s errors: local=%v hosted=%v", name, localErr, hostedErr)
		}
		left, _ := json.Marshal(localValue)
		right, _ := json.Marshal(hostedValue)
		if string(left) != string(right) {
			t.Fatalf("%s contract mismatch\nlocal:  %s\nhosted: %s", name, left, right)
		}
	}

	localStatus, localErr := local.Status(ctx)
	hostedStatus, hostedErr := hosted.Status(ctx)
	assertSame("status", localStatus, hostedStatus, localErr, hostedErr)
	localSearch, localErr := local.Search(ctx, "dashboard", "", "", 20, nil)
	hostedSearch, hostedErr := hosted.Search(ctx, "dashboard", "", "", 20, nil)
	assertSame("search", localSearch, hostedSearch, localErr, hostedErr)
	localEntities, localErr := local.ListEntities(ctx, "repository", "", "", 20, nil)
	hostedEntities, hostedErr := hosted.ListEntities(ctx, "repository", "", "", 20, nil)
	assertSame("entities", localEntities, hostedEntities, localErr, hostedErr)
	localArtifacts, localErr := local.ArtifactVersions(ctx, artifact.ID, "", 1)
	hostedArtifacts, hostedErr := hosted.ArtifactVersions(ctx, artifact.ID, "", 1)
	assertSame("artifact versions", localArtifacts, hostedArtifacts, localErr, hostedErr)
	localMemories, localErr := local.MemoryVersions(ctx, memory.ID, "", 1)
	hostedMemories, hostedErr := hosted.MemoryVersions(ctx, memory.ID, "", 1)
	assertSame("memory versions", localMemories, hostedMemories, localErr, hostedErr)
	localRelationships, localErr := local.Relationships(ctx, entity.ID, "", 20)
	hostedRelationships, hostedErr := hosted.Relationships(ctx, entity.ID, "", 20)
	assertSame("relationships", localRelationships, hostedRelationships, localErr, hostedErr)
}
