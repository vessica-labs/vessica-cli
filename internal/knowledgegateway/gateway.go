package knowledgegateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	q = normalizeMemorySearchQuery(q)
	if g.local != nil {
		return g.store.SearchMemories(ctx, g.workspace, scopes, q, 100)
	}
	return g.remote.SearchMemories(ctx, g.workspace, q, scopes)
}

func (g *Gateway) RetrieveMemories(ctx context.Context, r ks.MemoryRetrievalRequest) (ks.MemoryRetrievalResponse, error) {
	r.WorkspaceID = g.workspace
	r.Query = normalizeMemorySearchQuery(r.Query)
	if g.local != nil {
		return g.local.RetrieveMemories(ctx, r)
	}
	return g.remote.RetrieveMemories(ctx, r)
}

func normalizeMemorySearchQuery(q string) string {
	q = strings.NewReplacer("-", " ", "_", " ", "–", " ", "—", " ").Replace(q)
	return strings.Join(strings.Fields(q), " ")
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
	action := state
	switch state {
	case "archived":
		action = "archive"
	case "superseded":
		action = "supersede"
	}
	return g.remote.SetMemoryState(ctx, key, g.workspace, id, action)
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
func (g *Gateway) GetEntity(ctx context.Context, entityID string) (ks.Entity, error) {
	if g.local != nil {
		return g.store.GetEntity(ctx, g.workspace, entityID)
	}
	return g.remote.GetEntity(ctx, g.workspace, entityID)
}
func (g *Gateway) ListEntities(ctx context.Context, typ, state, cursor string, limit int, scopes []string) (ks.Page[ks.Entity], error) {
	if g.local == nil {
		return g.remote.ListEntities(ctx, g.workspace, typ, state, cursor, limit, scopes)
	}
	items, err := g.store.ListEntities(ctx, g.workspace, scopes, typ, state)
	if err != nil {
		return ks.Page[ks.Entity]{}, err
	}
	return localPage(items, cursor, limit), nil
}
func (g *Gateway) Status(ctx context.Context) (ks.Status, error) {
	if g.local == nil {
		return g.remote.Status(ctx, g.workspace)
	}
	backlog, err := g.store.EmbeddingBacklog(ctx, g.workspace)
	if err != nil {
		return ks.Status{}, err
	}
	return ks.Status{Schema: ks.APIVersion, RetrievalMode: "lexical", EmbeddingState: "not_configured", EmbeddingBacklog: backlog, IndexFresh: backlog == 0}, nil
}
func (g *Gateway) Search(ctx context.Context, query, objectType, cursor string, limit int, scopes []string) (ks.Page[ks.SearchResult], error) {
	if g.local == nil {
		return g.remote.Search(ctx, g.workspace, query, objectType, cursor, limit, scopes)
	}
	var out []ks.SearchResult
	if objectType == "" || objectType == "entity" {
		entities, err := g.store.ResolveEntities(ctx, g.workspace, scopes, query)
		if err != nil {
			return ks.Page[ks.SearchResult]{}, err
		}
		for _, v := range entities {
			out = append(out, ks.SearchResult{ObjectType: "entity", ID: v.ID, ScopeID: v.ScopeID, Subtype: v.Type, Title: v.DisplayName, State: v.State, UpdatedAt: v.UpdatedAt})
		}
	}
	if objectType == "" || objectType == "artifact" {
		artifacts, err := g.store.ListArtifacts(ctx, g.workspace, scopes, nil)
		if err != nil {
			return ks.Page[ks.SearchResult]{}, err
		}
		for _, v := range artifacts {
			if query == "" || strings.Contains(strings.ToLower(v.Title+" "+v.Content), strings.ToLower(query)) {
				out = append(out, ks.SearchResult{ObjectType: "artifact", ID: v.ID, ScopeID: v.ScopeID, Subtype: v.Type, Title: v.Title, Summary: summary(v.Content), State: v.Status, UpdatedAt: v.UpdatedAt})
			}
		}
	}
	if objectType == "" || objectType == "memory" {
		memories, err := g.store.SearchMemories(ctx, g.workspace, scopes, query, 500)
		if err != nil {
			return ks.Page[ks.SearchResult]{}, err
		}
		for _, v := range memories {
			out = append(out, ks.SearchResult{ObjectType: "memory", ID: v.ID, ScopeID: v.ScopeID, Subtype: v.Type, Title: v.Title, Summary: summary(v.Content), State: v.State, UpdatedAt: v.UpdatedAt})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return localPage(out, cursor, limit), nil
}
func (g *Gateway) ArtifactVersions(ctx context.Context, id, cursor string, limit int) (ks.Page[ks.Artifact], error) {
	if g.local == nil {
		return g.remote.ListArtifactVersions(ctx, g.workspace, id, cursor, limit)
	}
	items, err := g.store.ListArtifactVersions(ctx, g.workspace, id)
	if err != nil {
		return ks.Page[ks.Artifact]{}, err
	}
	return localPage(items, cursor, limit), nil
}
func (g *Gateway) MemoryVersions(ctx context.Context, id, cursor string, limit int) (ks.Page[ks.Memory], error) {
	if g.local == nil {
		return g.remote.ListMemoryVersions(ctx, g.workspace, id, cursor, limit)
	}
	items, err := g.store.ListMemoryVersions(ctx, g.workspace, id)
	if err != nil {
		return ks.Page[ks.Memory]{}, err
	}
	return localPage(items, cursor, limit), nil
}
func (g *Gateway) Relationships(ctx context.Context, objectID, cursor string, limit int) (ks.Page[ks.Relationship], error) {
	if g.local == nil {
		return g.remote.ListRelationships(ctx, g.workspace, objectID, cursor, limit)
	}
	items, err := g.store.ListRelationships(ctx, g.workspace, objectID)
	if err != nil {
		return ks.Page[ks.Relationship]{}, err
	}
	return localPage(items, cursor, limit), nil
}
func localPage[T any](items []T, cursor string, limit int) ks.Page[T] {
	offset := 0
	if raw, err := base64.RawURLEncoding.DecodeString(cursor); err == nil {
		offset, _ = strconv.Atoi(string(raw))
	}
	if offset < 0 || offset > len(items) {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	result := ks.Page[T]{Items: items[offset:end]}
	if end < len(items) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return result
}
func summary(v string) string {
	if len(v) <= 240 {
		return v
	}
	return v[:240] + "…"
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
