package knowledgegateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	knowledgeclient "github.com/vessica-labs/vessica-knowledge-server/client"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type Gateway struct {
	mode        string
	workspace   string
	local       *ks.Service
	store       ks.Store
	remote      *knowledgeclient.Client
	remoteAdmin *knowledgeclient.Client
}

func Open(root string, cfg config.Config, workspaceID string) (*Gateway, error) {
	return open(root, cfg, workspaceID, false)
}

func OpenForPromotion(root string, cfg config.Config, workspaceID string) (*Gateway, error) {
	return open(root, cfg, workspaceID, true)
}

func open(root string, cfg config.Config, workspaceID string, allowPromotionLock bool) (*Gateway, error) {
	mode := cfg.Knowledge.Mode
	if mode == "" {
		mode = "local"
	}
	kw := cfg.Knowledge.WorkspaceID
	if kw == "" {
		kw = workspaceID
	}
	g := &Gateway{mode: mode, workspace: kw}
	if mode == "hosted" {
		if cfg.Knowledge.Endpoint == "" {
			return nil, fmt.Errorf("knowledge.endpoint required in hosted mode")
		}
		token := os.Getenv("VES_KNOWLEDGE_TOKEN")
		if token == "" {
			var err error
			token, err = auth.Token("knowledge")
			if err != nil {
				return nil, err
			}
		}
		g.remote = &knowledgeclient.Client{BaseURL: cfg.Knowledge.Endpoint, Token: token, Actor: ks.Actor{ID: "ves-cli", Type: "user"}}
		adminToken, adminErr := auth.Token("knowledge-export")
		if adminErr != nil {
			adminToken = token
		}
		g.remoteAdmin = &knowledgeclient.Client{BaseURL: cfg.Knowledge.Endpoint, Token: adminToken, Actor: ks.Actor{ID: "ves-cli", Type: "user"}}
		return g, nil
	}
	if !allowPromotionLock {
		if _, err := os.Stat(filepath.Join(root, ".vessica", "state", "knowledge.promote.lock")); err == nil {
			return nil, fmt.Errorf("knowledge promotion is in progress; local writes and reads are temporarily frozen")
		}
	}
	path := cfg.Knowledge.LocalPath
	if path == "" {
		path = filepath.Join(".vessica", "state", "knowledge.db")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	store, err := ks.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	g.store = store
	g.local = ks.NewService(store, nil)
	return g, nil
}
func (g *Gateway) Close() error {
	if g.store != nil {
		return g.store.Close()
	}
	return nil
}
func (g *Gateway) Mode() string      { return g.mode }
func (g *Gateway) Workspace() string { return g.workspace }
func (g *Gateway) EnsureRepositoryScope(ctx context.Context, canonical, name string) (ks.Scope, error) {
	key := "repo:" + canonical
	if strings.TrimSpace(name) == "" {
		name = canonical
	}
	if g.local != nil {
		if got, err := g.store.GetScope(ctx, g.workspace, key); err == nil {
			return got, nil
		}
		return g.local.CreateScope(ctx, g.opts("scope:"+key), ks.Scope{Type: "repository", Name: name, CanonicalKey: key})
	}
	return g.remote.CreateScope(ctx, "scope:"+key, ks.Scope{WorkspaceID: g.workspace, Type: "repository", Name: name, CanonicalKey: key})
}
func (g *Gateway) opts(key string) ks.WriteOptions {
	return ks.WriteOptions{WorkspaceID: g.workspace, IdempotencyKey: key, Actor: ks.Actor{ID: "ves-cli", Type: "user"}, Provenance: ks.Provenance{Source: "vessica_cli"}}
}
func (g *Gateway) Context(ctx context.Context, r ks.ContextRequest) (ks.ContextResponse, error) {
	r.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.Context(ctx, r)
	}
	return g.remote.Context(ctx, r)
}
func (g *Gateway) CreateMemory(ctx context.Context, key string, v ks.Memory) (ks.Memory, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.CreateMemory(ctx, g.opts(key), v)
	}
	return g.remote.CreateMemory(ctx, key, v)
}
func (g *Gateway) GetMemory(ctx context.Context, id string) (ks.Memory, error) {
	if g.local != nil {
		return g.store.GetMemory(ctx, g.workspace, id, 0)
	}
	return g.remote.GetMemory(ctx, g.workspace, id, 0)
}
func (g *Gateway) SearchMemories(ctx context.Context, q string, scopes []string) ([]ks.Memory, error) {
	if g.local != nil {
		return g.store.SearchMemories(ctx, g.workspace, scopes, q, 100)
	}
	return g.remote.SearchMemories(ctx, g.workspace, q, scopes)
}
func (g *Gateway) VersionMemory(ctx context.Context, key string, v ks.Memory) (ks.Memory, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.VersionMemory(ctx, g.opts(key), v.ID, v)
	}
	return g.remote.VersionMemory(ctx, key, v)
}
func (g *Gateway) SetMemoryState(ctx context.Context, key, id, state string) (ks.Memory, error) {
	if g.local != nil {
		return g.local.SetMemoryState(ctx, g.opts(key), id, state)
	}
	return g.remote.SetMemoryState(ctx, key, g.workspace, id, state)
}
func (g *Gateway) CreateArtifact(ctx context.Context, key string, v ks.Artifact) (ks.Artifact, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.CreateArtifact(ctx, g.opts(key), v)
	}
	return g.remote.CreateArtifact(ctx, key, v)
}
func (g *Gateway) GetArtifact(ctx context.Context, id string) (ks.Artifact, error) {
	if g.local != nil {
		return g.store.GetArtifact(ctx, g.workspace, id, 0)
	}
	return g.remote.GetArtifact(ctx, g.workspace, id, 0)
}
func (g *Gateway) ListArtifacts(ctx context.Context, typ, status string, scopes []string) ([]ks.Artifact, error) {
	if g.local != nil {
		return g.store.ListArtifacts(ctx, g.workspace, scopes, []ks.ArtifactSelector{{Type: typ, Status: status}})
	}
	return g.remote.ListArtifacts(ctx, g.workspace, typ, status, scopes)
}
func (g *Gateway) VersionArtifact(ctx context.Context, key string, v ks.Artifact) (ks.Artifact, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.VersionArtifact(ctx, g.opts(key), v.ID, v)
	}
	return g.remote.VersionArtifact(ctx, key, v)
}
func (g *Gateway) SetArtifactStatus(ctx context.Context, key, id, status string) (ks.Artifact, error) {
	if g.local != nil {
		return g.local.SetArtifactStatus(ctx, g.opts(key), id, status)
	}
	return g.remote.SetArtifactStatus(ctx, key, g.workspace, id, status)
}
func (g *Gateway) CreateEntity(ctx context.Context, key string, v ks.Entity) (ks.Entity, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.CreateEntity(ctx, g.opts(key), v)
	}
	return g.remote.CreateEntity(ctx, key, v)
}
func (g *Gateway) ResolveEntities(ctx context.Context, q string, scopes []string) ([]ks.Entity, error) {
	if g.local != nil {
		return g.store.ResolveEntities(ctx, g.workspace, scopes, q)
	}
	return g.remote.ResolveEntities(ctx, g.workspace, q, scopes)
}
func (g *Gateway) Workflow(ctx context.Context, key string, v ks.WorkflowEvent) (ks.Memory, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.IngestWorkflowEvent(ctx, g.opts(key), v)
	}
	return g.remote.IngestWorkflowEvent(ctx, key, v)
}
func (g *Gateway) CreateRelationship(ctx context.Context, key string, v ks.Relationship) (ks.Relationship, error) {
	v.WorkspaceID = g.workspace
	if g.local != nil {
		return g.local.CreateRelationship(ctx, g.opts(key), v)
	}
	return g.remote.CreateRelationship(ctx, key, v)
}
func (g *Gateway) Export(ctx context.Context) (ks.Snapshot, error) {
	if g.local != nil {
		return g.store.Export(ctx, g.workspace)
	}
	return g.remoteAdmin.Export(ctx, g.workspace)
}
func (g *Gateway) Import(ctx context.Context, v ks.Snapshot) error {
	if g.local != nil {
		return g.store.Import(ctx, v)
	}
	return g.remoteAdmin.Import(ctx, v)
}
func CanonicalRepository(remote, root string) string {
	v := strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if v == "" {
		v = root
	}
	return v
}
