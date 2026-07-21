package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/version"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

const APISchema = "vessica.dashboard/v1"

type Service struct {
	DB     *state.DB
	Root   string
	Config config.Config
}
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}
type SystemStatus struct {
	Schema           string              `json:"schema"`
	Mode             string              `json:"mode"`
	WorkspaceID      string              `json:"workspace_id"`
	WorkspaceProfile string              `json:"workspace_profile"`
	Version          string              `json:"version"`
	DashboardVersion string              `json:"dashboard_version"`
	Database         map[string]any      `json:"database"`
	Knowledge        map[string]any      `json:"knowledge"`
	Integrations     []map[string]any    `json:"integrations"`
	Counts           map[string]int      `json:"counts"`
	Warnings         []map[string]string `json:"warnings"`
	Repositories     []state.Repository  `json:"repositories"`
	AgentRuntime     map[string]any      `json:"agent_runtime,omitempty"`
}
type RunDetail struct {
	Run       *state.Run          `json:"run"`
	Epic      any                 `json:"epic,omitempty"`
	Tickets   any                 `json:"tickets"`
	Phases    []state.RunPhase    `json:"phases"`
	Sandboxes []state.Sandbox     `json:"sandboxes"`
	Artifacts []state.Artifact    `json:"artifacts"`
	Evidence  []state.RunEvidence `json:"evidence"`
	Receipt   any                 `json:"receipt,omitempty"`
}

func New(db *state.DB, root string, cfg config.Config) *Service {
	return &Service{DB: db, Root: root, Config: cfg}
}
func (s *Service) mode() string {
	if s.Config.State.Backend == "postgres" || s.Config.State.Backend == "postgres-url" || s.Config.Hosted.Provider != "" {
		return "hosted"
	}
	return "local"
}

func (s *Service) System(ctx context.Context) (*SystemStatus, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("database is required")
	}
	ws, err := s.DB.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	result := &SystemStatus{Schema: APISchema, Mode: s.mode(), WorkspaceID: ws.ID, WorkspaceProfile: ws.Profile, Version: version.Version, DashboardVersion: version.Version, Database: map[string]any{"status": "ready", "backend": s.DB.Dialect, "migrations": "ready"}, Counts: map[string]int{}, Warnings: []map[string]string{}}
	result.Repositories, _ = s.DB.ListRepositories(ctx)
	if err := s.DB.Ping(ctx); err != nil {
		result.Database = map[string]any{"status": "unavailable", "backend": s.DB.Dialect}
		result.Warnings = append(result.Warnings, map[string]string{"code": "database_unavailable", "message": err.Error()})
	}
	if runs, e := s.DB.ListRuns(ctx); e == nil {
		result.Counts["runs"] = len(runs)
		for _, v := range runs {
			result.Counts["runs_"+v.Status]++
		}
	}
	if sandboxes, e := s.DB.ListSandboxes(ctx); e == nil {
		result.Counts["sandboxes"] = len(sandboxes)
		for _, v := range sandboxes {
			result.Counts["sandboxes_"+v.Status]++
		}
	}
	for _, provider := range []string{"linear", "jira"} {
		if v, e := s.DB.GetTrackerIntegration(ctx, provider); e == nil {
			result.Integrations = append(result.Integrations, map[string]any{"provider": provider, "status": v.Status, "last_synced_at": v.LastSyncedAt, "last_error": v.LastError})
		} else if provider == s.Config.Tracker.Provider {
			result.Integrations = append(result.Integrations, map[string]any{"provider": provider, "status": "not_configured"})
		}
	}
	if s.Config.Tracker.Provider != "jira" {
		result.Integrations = append(result.Integrations, map[string]any{"provider": "jira", "status": "unsupported"})
	}
	result.Integrations = append(result.Integrations, map[string]any{"provider": s.Config.Repo.Provider, "status": map[bool]string{true: "configured", false: "not_configured"}[strings.TrimSpace(s.Config.Repo.Remote) != ""]})
	g, e := s.knowledge(ctx)
	if e != nil {
		result.Knowledge = map[string]any{"status": "unavailable", "error": e.Error()}
		result.Warnings = append(result.Warnings, map[string]string{"code": "knowledge_unavailable", "message": e.Error()})
	} else {
		defer g.Close()
		status, e := g.Status(ctx)
		if e != nil {
			result.Knowledge = map[string]any{"status": "unavailable", "error": e.Error()}
		} else {
			raw, _ := json.Marshal(status)
			_ = json.Unmarshal(raw, &result.Knowledge)
			result.Knowledge["status"] = "ready"
		}
	}
	return result, nil
}
func (s *Service) Runs(ctx context.Context, cursor string, limit int) (Page[state.Run], error) {
	items, err := s.DB.ListRuns(ctx)
	if err != nil {
		return Page[state.Run]{}, err
	}
	return paginate(items, cursor, limit), nil
}
func (s *Service) RunsForRepository(ctx context.Context, repositoryID, cursor string, limit int) (Page[state.Run], error) {
	items, err := s.DB.ListRunsForRepository(ctx, repositoryID)
	if err != nil {
		return Page[state.Run]{}, err
	}
	return paginate(items, cursor, limit), nil
}
func (s *Service) Sandboxes(ctx context.Context, cursor string, limit int) (Page[state.Sandbox], error) {
	items, err := s.DB.ListSandboxes(ctx)
	if err != nil {
		return Page[state.Sandbox]{}, err
	}
	return paginate(items, cursor, limit), nil
}
func (s *Service) Run(ctx context.Context, runID string) (*RunDetail, error) {
	r, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	d := &RunDetail{Run: r}
	d.Phases, _ = s.DB.ListPhases(ctx, runID)
	d.Sandboxes, _ = s.DB.ListSandboxesForRun(ctx, runID)
	d.Artifacts, _ = s.DB.ListArtifactsForRun(ctx, runID)
	d.Evidence, _ = s.DB.ListRunEvidence(ctx, runID)
	if r.EpicID != "" {
		if e, x := s.DB.GetEpic(ctx, r.EpicID); x == nil {
			d.Epic = e
		}
		if tickets, x := s.DB.ListTicketsForRun(ctx, r.EpicID, runID); x == nil {
			d.Tickets = tickets
		}
	}
	if r.ReceiptID != "" {
		if rc, x := s.DB.GetReceipt(ctx, r.ReceiptID); x == nil {
			if view, y := receipt.ViewJSON(rc); y == nil {
				d.Receipt = view
			}
		}
	}
	return d, nil
}
func (s *Service) Event(ctx context.Context, eventID string) (*state.Event, error) {
	return s.DB.GetEvent(ctx, eventID)
}
func (s *Service) Events(ctx context.Context, runID string, after int64) ([]state.Event, error) {
	events, err := s.DB.ListEvents(ctx, runID, after)
	if err != nil {
		return nil, err
	}
	const batchSize = 500
	if len(events) > batchSize {
		events = events[:batchSize]
	}
	return events, nil
}

func (s *Service) RawLog(ctx context.Context, runID string) (map[string]any, error) {
	if _, err := s.DB.GetRun(ctx, runID); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, ".vessica", "runs", runID, "agent.jsonl")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("raw log is unavailable for run %s", runID)
		}
		return nil, err
	}
	defer file.Close()
	const limit = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	truncated := len(raw) > limit
	if truncated {
		raw = raw[:limit]
	}
	return map[string]any{"run_id": runID, "content": redaction.Redact(string(raw)), "truncated": truncated}, nil
}

func (s *Service) Refine(ctx context.Context, runID, prompt string) (any, error) {
	sb, err := s.DB.GetSandboxForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	engine := &runengine.Engine{DB: s.DB, Root: s.Root, Config: s.Config}
	return engine.PromptSandbox(ctx, sb.ID, runengine.PromptOptions{Prompt: prompt, Push: true})
}
func (s *Service) Approve(ctx context.Context, runID string) (any, error) {
	engine := &runengine.Engine{DB: s.DB, Root: s.Root, Config: s.Config}
	return engine.ApproveRun(ctx, runID, runengine.ApproveOptions{MergeMethod: "squash"})
}
func (s *Service) Rollback(ctx context.Context, runID string) (any, error) {
	engine := &runengine.Engine{DB: s.DB, Root: s.Root, Config: s.Config}
	return engine.RollbackRun(ctx, runID)
}
func (s *Service) Cancel(ctx context.Context, runID string) (*state.Run, error) {
	return NewRunLifecycle(s.DB, s.Root, s.Config, nil).Cancel(ctx, runID, "dashboard")
}
func (s *Service) Retain(ctx context.Context, sandboxID string, duration time.Duration) (*state.Sandbox, error) {
	return NewRunLifecycle(s.DB, s.Root, s.Config, nil).Retain(ctx, sandboxID, duration)
}
func (s *Service) Destroy(ctx context.Context, sandboxID string) (*state.Sandbox, error) {
	return NewRunLifecycle(s.DB, s.Root, s.Config, nil).Destroy(ctx, sandboxID, "dashboard")
}

func (s *Service) knowledge(ctx context.Context) (*knowledgegateway.Gateway, error) {
	ws, err := s.DB.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return knowledgegateway.Open(s.Root, s.Config, ws.ID)
}
func (s *Service) KnowledgeStatus(ctx context.Context) (knowledge.Status, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Status{}, err
	}
	defer g.Close()
	return g.Status(ctx)
}
func (s *Service) KnowledgeSearch(ctx context.Context, q, typ, cursor string, limit int, scopes []string) (knowledge.Page[knowledge.SearchResult], error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Page[knowledge.SearchResult]{}, err
	}
	defer g.Close()
	return g.Search(ctx, q, typ, cursor, limit, scopes)
}
func (s *Service) Entities(ctx context.Context, typ, stateValue, cursor string, limit int, scopes []string) (knowledge.Page[knowledge.Entity], error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Page[knowledge.Entity]{}, err
	}
	defer g.Close()
	return g.ListEntities(ctx, typ, stateValue, cursor, limit, scopes)
}
func (s *Service) Entity(ctx context.Context, id string) (knowledge.Entity, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Entity{}, err
	}
	defer g.Close()
	return g.GetEntity(ctx, id)
}
func (s *Service) Artifacts(ctx context.Context, typ, status string, scopes []string) ([]knowledge.Artifact, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	return g.ListArtifacts(ctx, typ, status, scopes)
}
func (s *Service) Artifact(ctx context.Context, id string) (knowledge.Artifact, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Artifact{}, err
	}
	defer g.Close()
	return g.GetArtifact(ctx, id)
}
func (s *Service) ArtifactVersions(ctx context.Context, id, cursor string, limit int) (knowledge.Page[knowledge.Artifact], error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Page[knowledge.Artifact]{}, err
	}
	defer g.Close()
	return g.ArtifactVersions(ctx, id, cursor, limit)
}
func (s *Service) Memories(ctx context.Context, q string, scopes []string) ([]knowledge.Memory, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Close()
	return g.SearchMemories(ctx, q, scopes)
}
func (s *Service) Memory(ctx context.Context, id string) (knowledge.Memory, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Memory{}, err
	}
	defer g.Close()
	return g.GetMemory(ctx, id)
}
func (s *Service) MemoryVersions(ctx context.Context, id, cursor string, limit int) (knowledge.Page[knowledge.Memory], error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Page[knowledge.Memory]{}, err
	}
	defer g.Close()
	return g.MemoryVersions(ctx, id, cursor, limit)
}
func (s *Service) Relationships(ctx context.Context, id, cursor string, limit int) (knowledge.Page[knowledge.Relationship], error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.Page[knowledge.Relationship]{}, err
	}
	defer g.Close()
	return g.Relationships(ctx, id, cursor, limit)
}
func (s *Service) Explain(ctx context.Context, request knowledge.ContextRequest) (knowledge.ContextResponse, error) {
	g, err := s.knowledge(ctx)
	if err != nil {
		return knowledge.ContextResponse{}, err
	}
	defer g.Close()
	return g.Context(ctx, request)
}

func paginate[T any](items []T, cursor string, limit int) Page[T] {
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
	result := Page[T]{Items: items[offset:end]}
	if end < len(items) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
	}
	return result
}
