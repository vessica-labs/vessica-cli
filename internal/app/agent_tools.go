package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

// EnsureRepositoryKnowledgeScope returns the durable knowledge scope owned by
// a repository. Agent runtimes use it so models never need to invent opaque
// workspace or scope identifiers.
func (s *Service) EnsureRepositoryKnowledgeScope(ctx context.Context, repositoryID string) (knowledge.Scope, error) {
	repository, err := s.DB.GetRepository(ctx, repositoryID)
	if err != nil {
		return knowledge.Scope{}, err
	}
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Scope{}, err
	}
	defer g.Close()
	canonical := knowledgegateway.CanonicalRepository(repository.CanonicalRemote, s.Root)
	return g.EnsureRepositoryScope(ctx, canonical, repository.DisplayName)
}

// ExecuteAgentTool keeps knowledge credentials inside the control plane. The
// runtime supplies only a fenced, audited invocation and typed JSON input.
func (s *Service) ExecuteAgentTool(ctx context.Context, toolID, key, repositoryScopeID string, args json.RawMessage) (any, error) {
	g, err := s.knowledge(ctx)
	knowledgeTool := toolID == "knowledge.retrieve" || strings.HasPrefix(toolID, "memory.") || strings.HasPrefix(toolID, "entity.") || strings.HasPrefix(toolID, "artifact.")
	if knowledgeTool && err != nil {
		return nil, err
	}
	if g != nil {
		defer g.Close()
	}
	switch toolID {
	case "knowledge.retrieve":
		var v knowledge.ContextRequest
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.Context(ctx, v)
	case "artifact.list":
		var v struct {
			Type   string   `json:"type"`
			Status string   `json:"status"`
			Scopes []string `json:"scopes"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.ListArtifacts(ctx, v.Type, v.Status, v.Scopes)
	case "artifact.get":
		var v struct {
			ArtifactID string `json:"artifact_id"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.GetArtifact(ctx, v.ArtifactID)
	case "artifact.create":
		var v knowledge.Artifact
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		v.ScopeID = repositoryScopeID
		return g.CreateArtifact(ctx, key, v)
	case "artifact.version":
		var v knowledge.Artifact
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		var ref struct {
			ArtifactID string `json:"artifact_id"`
		}
		_ = json.Unmarshal(args, &ref)
		v.ID = ref.ArtifactID
		v.ScopeID = repositoryScopeID
		return g.VersionArtifact(ctx, key, v)
	case "artifact.activate":
		return agentArtifactState(ctx, g, key, args, "active")
	case "artifact.supersede":
		return agentArtifactState(ctx, g, key, args, "superseded")
	case "memory.list", "memory.search":
		var v struct {
			Query  string   `json:"query"`
			Scopes []string `json:"scopes"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.SearchMemories(ctx, v.Query, v.Scopes)
	case "memory.get":
		var v struct {
			MemoryID string `json:"memory_id"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.GetMemory(ctx, v.MemoryID)
	case "memory.create":
		var v knowledge.Memory
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		v.ScopeID = repositoryScopeID
		return g.CreateMemory(ctx, key, v)
	case "memory.version":
		var v knowledge.Memory
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		var ref struct {
			MemoryID string `json:"memory_id"`
		}
		_ = json.Unmarshal(args, &ref)
		v.ID = ref.MemoryID
		v.ScopeID = repositoryScopeID
		return g.VersionMemory(ctx, key, v)
	case "memory.supersede":
		return agentMemoryState(ctx, g, key, args, "superseded")
	case "memory.archive":
		return agentMemoryState(ctx, g, key, args, "archived")
	case "entity.get":
		var v struct {
			EntityID string `json:"entity_id"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.GetEntity(ctx, v.EntityID)
	case "entity.resolve":
		var v struct {
			Query  string   `json:"query"`
			Scopes []string `json:"scopes"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return g.ResolveEntities(ctx, v.Query, v.Scopes)
	case "entity.create":
		var v knowledge.Entity
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		v.ScopeID = repositoryScopeID
		return g.CreateEntity(ctx, key, v)
	case "epic.list":
		return s.DB.ListEpics(ctx)
	case "epic.view":
		var v struct {
			EpicID string `json:"epic_id"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return s.DB.GetEpic(ctx, v.EpicID)
	case "epic.create":
		var v struct {
			RepositoryID string `json:"repository_id"`
			Title        string `json:"title"`
			Body         string `json:"body"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return s.DB.CreateEpicForRepository(ctx, v.RepositoryID, v.Title, v.Body)
	case "coding_run.status":
		var v struct {
			RunID string `json:"run_id"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return s.DB.GetRun(ctx, v.RunID)
	case "coding_run.events":
		var v struct {
			RunID string `json:"run_id"`
			After int64  `json:"after"`
		}
		if err = json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		return s.DB.ListEvents(ctx, v.RunID, v.After)
	default:
		return nil, fmt.Errorf("tool %s is not implemented by this application service", toolID)
	}
}

func agentArtifactState(ctx context.Context, g interface {
	SetArtifactStatus(context.Context, string, string, string) (knowledge.Artifact, error)
}, key string, args json.RawMessage, status string) (any, error) {
	var v struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	return g.SetArtifactStatus(ctx, key, v.ArtifactID, status)
}
func agentMemoryState(ctx context.Context, g interface {
	SetMemoryState(context.Context, string, string, string) (knowledge.Memory, error)
}, key string, args json.RawMessage, state string) (any, error) {
	var v struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	return g.SetMemoryState(ctx, key, v.MemoryID, state)
}
