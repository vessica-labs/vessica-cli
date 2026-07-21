package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

//go:embed all:assets all:docs
var embeddedAssets embed.FS

const sessionCookie = "ves_dashboard"

type Server struct {
	App            *appservice.Service
	DB             *state.DB
	Mode           string
	Origin         string
	PreviewOrigin  string
	ServiceToken   string
	GitHubClientID string
	Assets         fs.FS
	Promotion      func(context.Context, *state.HostingOperation) error
	PreviewAccess  func(context.Context, string) (string, error)
	RefineAction   func(context.Context, string, string) (any, error)
	ApproveAction  func(context.Context, string) (any, error)
	RollbackAction func(context.Context, string) (any, error)
	CancelAction   func(context.Context, string) (any, error)
	RetainAction   func(context.Context, string, time.Duration) (any, error)
	DestroyAction  func(context.Context, string) (any, error)
	RuntimeStatus  func() map[string]any
	mu             sync.Mutex
	mutationMu     sync.Mutex
	launch         map[string]time.Time
	githubFlows    map[string]githubFlow
	metrics        metrics
}
type envelope struct {
	Schema string         `json:"schema"`
	Data   any            `json:"data,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	Error  *apiError      `json:"error,omitempty"`
}
type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Details   any    `json:"details,omitempty"`
}
type actor struct {
	UserID, Role, SessionID string
	Service                 bool
}
type contextKey string

const actorKey contextKey = "dashboard-actor"

func New(app *appservice.Service, mode string) *Server {
	assets, _ := fs.Sub(embeddedAssets, "assets")
	return &Server{App: app, DB: app.DB, Mode: mode, Assets: assets, launch: map[string]time.Time{}, githubFlows: map[string]githubFlow{}}
}
func token(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func same(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) IssueLaunchToken() string {
	raw := token(32)
	s.mu.Lock()
	s.launch[digest(raw)] = time.Now().Add(2 * time.Minute)
	s.mu.Unlock()
	return raw
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /auth/local/launch", s.handleLocalLaunch)
	mux.HandleFunc("POST /auth/local/exchange", s.handleLocalExchange)
	mux.HandleFunc("GET /auth/config", s.handleAuthConfig)
	mux.HandleFunc("POST /auth/github/device", s.handleGitHubDevice)
	mux.HandleFunc("POST /auth/github/device/{id}/poll", s.handleGitHubPoll)
	mux.HandleFunc("GET /auth/session", s.withSession(s.handleSession))
	mux.HandleFunc("POST /auth/logout", s.withMutation("member", s.handleLogout))
	mux.HandleFunc("GET /api/v1/system", s.withSession(s.handleSystem))
	mux.HandleFunc("GET /api/v1/integrations", s.withSession(s.handleIntegrations))
	mux.HandleFunc("GET /api/v1/runs", s.withSession(s.handleRuns))
	mux.HandleFunc("GET /api/v1/runs/{id}", s.withSession(s.handleRun))
	mux.HandleFunc("GET /api/v1/runs/{id}/stream", s.withSession(s.handleRunStream))
	mux.HandleFunc("GET /api/v1/runs/{id}/events/{event_id}", s.withSession(s.handleEvent))
	mux.HandleFunc("GET /api/v1/runs/{id}/logs", s.withRole("owner", s.handleRawLog))
	mux.HandleFunc("POST /api/v1/runs/{id}/refinements", s.withMutation("member", s.handleRefinement))
	mux.HandleFunc("POST /api/v1/runs/{id}/approve", s.withMutation("member", s.handleApprove))
	mux.HandleFunc("POST /api/v1/runs/{id}/rollback", s.withMutation("member", s.handleRollback))
	mux.HandleFunc("POST /api/v1/runs/{id}/cancel", s.withMutation("member", s.handleCancel))
	mux.HandleFunc("POST /api/v1/runs/{id}/preview-access", s.withMutation("member", s.handlePreviewAccess))
	mux.HandleFunc("GET /api/v1/agents", s.withSession(s.handleAgents))
	mux.HandleFunc("POST /api/v1/agents", s.withMutation("owner", s.handleCreateAgent))
	mux.HandleFunc("GET /api/v1/agents/{id}", s.withSession(s.handleAgent))
	mux.HandleFunc("PATCH /api/v1/agents/{id}", s.withMutation("owner", s.handleUpdateAgent))
	mux.HandleFunc("POST /api/v1/agents/{id}/pause", s.withMutation("owner", s.handleAgentState))
	mux.HandleFunc("POST /api/v1/agents/{id}/resume", s.withMutation("owner", s.handleAgentState))
	mux.HandleFunc("POST /api/v1/agents/{id}/archive", s.withMutation("owner", s.handleAgentState))
	mux.HandleFunc("PUT /api/v1/agents/{id}/budget", s.withMutation("owner", s.handleAgentBudget))
	mux.HandleFunc("PUT /api/v1/agents/{id}/heartbeat", s.withMutation("owner", s.handleAgentHeartbeat))
	mux.HandleFunc("DELETE /api/v1/agents/{id}/heartbeat", s.withMutation("owner", s.handleAgentHeartbeat))
	mux.HandleFunc("POST /api/v1/agents/{id}/runs", s.withMutation("member", s.handleStartAgentRun))
	mux.HandleFunc("GET /api/v1/agent-builds/{id}", s.withSession(s.handleAgentBuild))
	mux.HandleFunc("GET /api/v1/agent-builds", s.withSession(s.handleAgentBuilds))
	mux.HandleFunc("POST /api/v1/agent-builds", s.withMutation("owner", s.handleCreateAgentBuild))
	mux.HandleFunc("POST /api/v1/agent-builds/{id}/activate", s.withMutation("owner", s.handleActivateAgentBuild))
	mux.HandleFunc("GET /api/v1/agent-runs", s.withSession(s.handleAgentRuns))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}", s.withSession(s.handleAgentRun))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}/events", s.withSession(s.handleAgentRunEvents))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}/stream", s.withSession(s.handleAgentRunStream))
	mux.HandleFunc("POST /api/v1/agent-runs/{id}/cancel", s.withMutation("member", s.handleCancelAgentRun))
	mux.HandleFunc("GET /api/v1/agent-tools", s.withSession(s.handleAgentTools))
	mux.HandleFunc("GET /api/v1/sandboxes", s.withSession(s.handleSandboxes))
	mux.HandleFunc("POST /api/v1/sandboxes/{id}/retain", s.withMutation("member", s.handleRetain))
	mux.HandleFunc("POST /api/v1/sandboxes/{id}/destroy", s.withMutation("member", s.handleDestroy))
	mux.HandleFunc("GET /api/v1/knowledge/status", s.withSession(s.handleKnowledgeStatus))
	mux.HandleFunc("GET /api/v1/knowledge/search", s.withSession(s.handleKnowledgeSearch))
	mux.HandleFunc("GET /api/v1/knowledge/entities", s.withSession(s.handleEntities))
	mux.HandleFunc("GET /api/v1/knowledge/entities/{id}", s.withSession(s.handleEntity))
	mux.HandleFunc("GET /api/v1/knowledge/artifacts", s.withSession(s.handleArtifacts))
	mux.HandleFunc("GET /api/v1/knowledge/artifacts/{id}", s.withSession(s.handleArtifact))
	mux.HandleFunc("GET /api/v1/knowledge/artifacts/{id}/versions", s.withSession(s.handleArtifactVersions))
	mux.HandleFunc("GET /api/v1/knowledge/memories", s.withSession(s.handleMemories))
	mux.HandleFunc("GET /api/v1/knowledge/memories/{id}", s.withSession(s.handleMemory))
	mux.HandleFunc("GET /api/v1/knowledge/memories/{id}/versions", s.withSession(s.handleMemoryVersions))
	mux.HandleFunc("GET /api/v1/knowledge/relationships", s.withSession(s.handleRelationships))
	mux.HandleFunc("POST /api/v1/knowledge/context:explain", s.withMutation("member", s.handleExplain))
	mux.HandleFunc("GET /api/v1/docs", s.withSession(s.handleDocs))
	mux.HandleFunc("GET /api/v1/docs/{slug}", s.withSession(s.handleDoc))
	mux.HandleFunc("GET /api/v1/access/members", s.withRole("owner", s.handleMembers))
	mux.HandleFunc("POST /api/v1/access/invitations", s.withMutation("owner", s.handleInvitation))
	mux.HandleFunc("POST /api/v1/access/owner-claims", s.withMutation("owner", s.handleOwnerClaim))
	mux.HandleFunc("GET /api/v1/audit", s.withRole("owner", s.handleAudit))
	mux.HandleFunc("GET /internal/dashboard/metrics", s.withRole("owner", s.metrics.serve))
	mux.Handle("/", s.spa())
	return s.security(mux)
}
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.ok(w, map[string]any{"ok": true, "mode": s.Mode}, nil)
}
func (s *Server) handleLocalLaunch(w http.ResponseWriter, r *http.Request) {
	if s.Mode != "local" || r.Header.Get("X-Vessica-CLI") != "1" {
		s.fail(w, r, http.StatusForbidden, "forbidden", "local CLI launch required", nil)
		return
	}
	host := r.RemoteAddr
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "[::1]:") {
		s.fail(w, r, http.StatusForbidden, "forbidden", "loopback request required", nil)
		return
	}
	s.ok(w, map[string]any{"launch_token": s.IssueLaunchToken()}, nil)
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		s.metrics.requests.Add(1)
		defer func() { s.metrics.durationNanos.Add(time.Since(started).Nanoseconds()) }()
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			rid = id.New("req")
		}
		w.Header().Set("X-Request-ID", rid)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-src "+cspOrigin(s.PreviewOrigin)+"; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
func cspOrigin(v string) string {
	if v == "" {
		return "'self'"
	}
	return v
}

func (s *Server) withSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := s.authenticate(r)
		if err != nil {
			s.fail(w, r, http.StatusUnauthorized, "unauthorized", err.Error(), nil)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), actorKey, a)))
	}
}
func (s *Server) withRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return s.withSession(func(w http.ResponseWriter, r *http.Request) {
		a := currentActor(r)
		if role == "owner" && a.Role != "owner" && !a.Service {
			s.fail(w, r, http.StatusForbidden, "forbidden", "owner role required", nil)
			return
		}
		next(w, r)
	})
}
func (s *Server) withMutation(role string, next http.HandlerFunc) http.HandlerFunc {
	return s.withRole(role, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			a := currentActor(r)
			if !a.Service {
				session, err := s.DB.GetDashboardSession(r.Context(), digest(cookieValue(r, sessionCookie)))
				if err != nil || !same(session.CSRFHash, digest(r.Header.Get("X-CSRF-Token"))) {
					s.fail(w, r, http.StatusForbidden, "csrf_failed", "valid CSRF token required", nil)
					return
				}
				if s.Origin != "" {
					got := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
					want := strings.TrimRight(s.Origin, "/")
					if got == "" || got != want {
						s.fail(w, r, http.StatusForbidden, "origin_mismatch", "request origin is not allowed", nil)
						return
					}
				}
			}
			if r.Header.Get("Idempotency-Key") == "" && r.URL.Path != "/auth/logout" {
				s.fail(w, r, http.StatusBadRequest, "idempotency_required", "Idempotency-Key header required", nil)
				return
			}
			s.mutationMu.Lock()
			defer s.mutationMu.Unlock()
			if r.Header.Get("Idempotency-Key") != "" && s.replayMutation(w, r) {
				return
			}
		}
		next(w, r)
	})
}
func currentActor(r *http.Request) actor { v, _ := r.Context().Value(actorKey).(actor); return v }
func cookieValue(r *http.Request, name string) string {
	v, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return v.Value
}
func (s *Server) authenticate(r *http.Request) (actor, error) {
	if header := r.Header.Get("Authorization"); s.ServiceToken != "" && strings.HasPrefix(header, "Bearer ") && same(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")), s.ServiceToken) {
		return actor{UserID: "service", Role: "owner", Service: true}, nil
	}
	raw := cookieValue(r, sessionCookie)
	if raw == "" {
		return actor{}, errors.New("dashboard session required")
	}
	v, err := s.DB.GetDashboardSession(r.Context(), digest(raw))
	if err != nil {
		return actor{}, err
	}
	return actor{UserID: v.UserID, Role: v.Role, SessionID: v.ID}, nil
}

func (s *Server) handleLocalExchange(w http.ResponseWriter, r *http.Request) {
	if s.Mode != "local" {
		s.fail(w, r, http.StatusNotFound, "not_found", "local exchange unavailable", nil)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	hash := digest(body.Token)
	s.mu.Lock()
	expiry, ok := s.launch[hash]
	if ok {
		delete(s.launch, hash)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(expiry) {
		s.fail(w, r, http.StatusUnauthorized, "invalid_launch_token", "launch token expired or invalid", nil)
		return
	}
	ws, err := s.DB.GetWorkspace(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	user, err := s.DB.UpsertDashboardUser(r.Context(), "local:"+ws.ID, "local", "Local owner", "")
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err = s.DB.UpsertMembership(r.Context(), user.ID, "owner"); err != nil {
		s.internal(w, r, err)
		return
	}
	sessionRaw, csrfRaw := token(32), token(32)
	session, err := s.DB.CreateDashboardSession(r.Context(), user.ID, "owner", digest(sessionRaw), digest(csrfRaw), time.Now().Add(12*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: sessionRaw, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(12 * time.Hour)})
	_ = s.DB.AppendDashboardAudit(r.Context(), user.ID, "session.login", "session", session.ID, r.Header.Get("X-Request-ID"), map[string]any{"mode": "local"})
	s.ok(w, map[string]any{"user": map[string]any{"id": user.ID, "login": user.Login, "role": "owner"}, "csrf_token": csrfRaw}, nil)
}
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	s.ok(w, map[string]any{"user_id": a.UserID, "role": a.Role, "mode": s.Mode}, nil)
}
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	_ = s.DB.DeleteDashboardSession(r.Context(), digest(cookieValue(r, sessionCookie)))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "session.logout", "session", a.SessionID, r.Header.Get("X-Request-ID"), nil)
	s.ok(w, map[string]any{"logged_out": true}, nil)
}

func (s *Server) spa() http.Handler {
	files := http.FileServer(http.FS(s.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, e := fs.Stat(s.Assets, path); e == nil && !f.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		raw, e := fs.ReadFile(s.Assets, "index.html")
		if e != nil {
			http.Error(w, "dashboard assets unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(raw)
	})
}
