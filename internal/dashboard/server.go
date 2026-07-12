package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
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
	mux.HandleFunc("POST /api/v1/hosting/promotions", s.withMutation("owner", s.handleStartPromotion))
	mux.HandleFunc("GET /api/v1/hosting/promotions/{id}", s.withRole("owner", s.handlePromotion))
	mux.HandleFunc("POST /api/v1/hosting/promotions/{id}/resume", s.withMutation("owner", s.handleResumePromotion))
	mux.HandleFunc("GET /api/v1/hosting/promotions/{id}/stream", s.withRole("owner", s.handlePromotionStream))
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

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.System(r.Context())
	s.respond(w, r, v, err)
}
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.System(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.ok(w, v.Integrations, nil)
}
func queryLimit(r *http.Request) int { n, _ := strconv.Atoi(r.URL.Query().Get("limit")); return n }
func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Runs(r.Context(), r.URL.Query().Get("cursor"), queryLimit(r))
	s.respond(w, r, v, err)
}
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Run(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, err)
}
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Event(r.Context(), r.PathValue("event_id"))
	if err == nil {
		v.PayloadJSON = redaction.Redact(v.PayloadJSON)
	}
	s.respond(w, r, v, err)
}
func (s *Server) handleRawLog(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.RawLog(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, err)
}
func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request) {
	s.metrics.sseActive.Add(1)
	defer s.metrics.sseActive.Add(-1)
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, 500, "stream_unavailable", "streaming unavailable", nil)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if n, e := strconv.ParseInt(raw, 10, 64); e == nil && n > after {
			after = n
		}
	}
	if after > 0 {
		s.metrics.sseReconnects.Add(1)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	poll := time.NewTicker(350 * time.Millisecond)
	heartbeat := time.NewTicker(12 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	runID := r.PathValue("id")
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-poll.C:
			events, err := s.App.Events(r.Context(), runID, after)
			if err != nil {
				return
			}
			for i := range events {
				event := events[i]
				event.PayloadJSON = redaction.Redact(event.PayloadJSON)
				after = event.Seq
				record := streaming.EventRecord(&event)
				raw, _ := json.Marshal(record)
				fmt.Fprintf(w, "id: %d\nevent: event\ndata: %s\n\n", event.Seq, raw)
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			runRecord, err := s.DB.GetRun(r.Context(), runID)
			if err == nil && (runRecord.Status == "completed" || runRecord.Status == "failed" || runRecord.Status == "cancelled") {
				record := streaming.ResultRecord(runID, runRecord, map[bool]error{true: fmt.Errorf("%s", runRecord.Error), false: nil}[runRecord.Status == "failed"])
				raw, _ := json.Marshal(record)
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", raw)
				flusher.Flush()
				return
			}
		}
	}
}

func (s *Server) handleRefinement(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt    string `json:"prompt"`
		Confirmed bool   `json:"confirmed"`
	}
	if !s.decode(w, r, &body, 64<<10) {
		return
	}
	if !body.Confirmed || len(strings.TrimSpace(body.Prompt)) < 1 || len(body.Prompt) > 4000 {
		s.fail(w, r, 400, "invalid_refinement", "confirmed prompt of 1-4000 characters required", nil)
		return
	}
	var v any
	var err error
	if s.RefineAction != nil {
		v, err = s.RefineAction(r.Context(), r.PathValue("id"), body.Prompt)
	} else {
		v, err = s.App.Refine(r.Context(), r.PathValue("id"), body.Prompt)
	}
	s.mutationResult(w, r, "run.refine", "run", r.PathValue("id"), v, err)
}
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	var v any
	var err error
	if s.ApproveAction != nil {
		v, err = s.ApproveAction(r.Context(), r.PathValue("id"))
	} else {
		v, err = s.App.Approve(r.Context(), r.PathValue("id"))
	}
	s.mutationResult(w, r, "run.approve", "run", r.PathValue("id"), v, err)
}
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	var v any
	var err error
	if s.RollbackAction != nil {
		v, err = s.RollbackAction(r.Context(), r.PathValue("id"))
	} else {
		v, err = s.App.Rollback(r.Context(), r.PathValue("id"))
	}
	s.mutationResult(w, r, "run.rollback", "run", r.PathValue("id"), v, err)
}
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	var v any
	var err error
	if s.CancelAction != nil {
		v, err = s.CancelAction(r.Context(), r.PathValue("id"))
	} else {
		v, err = s.App.Cancel(r.Context(), r.PathValue("id"))
	}
	s.mutationResult(w, r, "run.cancel", "run", r.PathValue("id"), v, err)
}
func (s *Server) handlePreviewAccess(w http.ResponseWriter, r *http.Request) {
	if s.PreviewAccess == nil {
		s.fail(w, r, 409, "preview_unavailable", "preview access is unavailable", nil)
		return
	}
	value, err := s.PreviewAccess(r.Context(), r.PathValue("id"))
	if err != nil {
		s.metrics.previewFailures.Add(1)
	}
	s.mutationResult(w, r, "preview.open", "run", r.PathValue("id"), map[string]any{"url": value}, err)
}
func (s *Server) handleSandboxes(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Sandboxes(r.Context(), r.URL.Query().Get("cursor"), queryLimit(r))
	s.respond(w, r, v, err)
}
func (s *Server) handleRetain(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hours     int  `json:"hours"`
		Confirmed bool `json:"confirmed"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	if !body.Confirmed || body.Hours <= 0 {
		s.fail(w, r, 400, "confirmation_required", "positive hours and confirmation required", nil)
		return
	}
	var v any
	var err error
	if s.RetainAction != nil {
		v, err = s.RetainAction(r.Context(), r.PathValue("id"), time.Duration(body.Hours)*time.Hour)
	} else {
		v, err = s.App.Retain(r.Context(), r.PathValue("id"), time.Duration(body.Hours)*time.Hour)
	}
	s.mutationResult(w, r, "sandbox.retain", "sandbox", r.PathValue("id"), v, err)
}
func (s *Server) handleDestroy(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	var v any
	var err error
	if s.DestroyAction != nil {
		v, err = s.DestroyAction(r.Context(), r.PathValue("id"))
	} else {
		v, err = s.App.Destroy(r.Context(), r.PathValue("id"))
	}
	s.mutationResult(w, r, "sandbox.destroy", "sandbox", r.PathValue("id"), v, err)
}
func (s *Server) confirmed(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Confirmed bool `json:"confirmed"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return false
	}
	if !body.Confirmed {
		s.fail(w, r, 400, "confirmation_required", "explicit confirmation required", nil)
		return false
	}
	return true
}
func (s *Server) mutationResult(w http.ResponseWriter, r *http.Request, action, targetType, targetID string, v any, err error) {
	a := currentActor(r)
	meta := map[string]any{"ok": err == nil, "idempotency_key": r.Header.Get("Idempotency-Key")}
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, action, targetType, targetID, r.Header.Get("X-Request-ID"), meta)
	if err == nil {
		_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), action, v)
	}
	s.respond(w, r, v, err)
}
func (s *Server) replayMutation(w http.ResponseWriter, r *http.Request) bool {
	a := currentActor(r)
	raw, ok, err := s.DB.GetDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.internal(w, r, err)
		return true
	}
	if !ok {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		value = json.RawMessage(raw)
	}
	s.ok(w, value, map[string]any{"replayed": true})
	return true
}

func (s *Server) handleKnowledgeStatus(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.KnowledgeStatus(r.Context())
	s.respond(w, r, v, e)
}
func (s *Server) handleKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.KnowledgeSearch(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("object_type"), r.URL.Query().Get("cursor"), queryLimit(r), r.URL.Query()["scope"])
	s.respond(w, r, v, e)
}
func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Entities(r.Context(), r.URL.Query().Get("type"), r.URL.Query().Get("state"), r.URL.Query().Get("cursor"), queryLimit(r), r.URL.Query()["scope"])
	s.respond(w, r, v, e)
}
func (s *Server) handleEntity(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Entity(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, e)
}
func (s *Server) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Artifacts(r.Context(), r.URL.Query().Get("type"), r.URL.Query().Get("status"), r.URL.Query()["scope"])
	s.respond(w, r, v, e)
}
func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Artifact(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, e)
}
func (s *Server) handleArtifactVersions(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.ArtifactVersions(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), queryLimit(r))
	s.respond(w, r, v, e)
}
func (s *Server) handleMemories(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Memories(r.Context(), r.URL.Query().Get("q"), r.URL.Query()["scope"])
	s.respond(w, r, v, e)
}
func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Memory(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, e)
}
func (s *Server) handleMemoryVersions(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.MemoryVersions(r.Context(), r.PathValue("id"), r.URL.Query().Get("cursor"), queryLimit(r))
	s.respond(w, r, v, e)
}
func (s *Server) handleRelationships(w http.ResponseWriter, r *http.Request) {
	v, e := s.App.Relationships(r.Context(), r.URL.Query().Get("object_id"), r.URL.Query().Get("cursor"), queryLimit(r))
	s.respond(w, r, v, e)
}
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	var body knowledge.ContextRequest
	if !s.decode(w, r, &body, 1<<20) {
		return
	}
	v, e := s.App.Explain(r.Context(), body)
	s.respond(w, r, v, e)
}
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	entries, e := fs.ReadDir(embeddedAssets, "docs")
	if e != nil {
		s.internal(w, r, e)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var out []map[string]any
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		raw, e := fs.ReadFile(embeddedAssets, "docs/"+entry.Name())
		if e != nil {
			continue
		}
		title := entry.Name()
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(title+" "+string(raw)), q) {
			continue
		}
		out = append(out, map[string]any{"slug": strings.TrimSuffix(entry.Name(), ".md"), "title": title, "bytes": len(raw)})
	}
	s.ok(w, out, nil)
}
func (s *Server) handleDoc(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		s.fail(w, r, 400, "invalid_slug", "invalid document slug", nil)
		return
	}
	raw, e := fs.ReadFile(embeddedAssets, "docs/"+slug+".md")
	if e != nil {
		s.fail(w, r, 404, "not_found", "document not found", nil)
		return
	}
	s.ok(w, map[string]any{"slug": slug, "markdown": string(raw)}, nil)
}

func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.ListMemberships(r.Context())
	s.respond(w, r, v, e)
}
func (s *Server) handleInvitation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GitHubLogin string `json:"github_login"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	login := strings.TrimSpace(body.GitHubLogin)
	if login == "" {
		s.fail(w, r, 400, "invalid_login", "GitHub login required", nil)
		return
	}
	raw := token(32)
	a := currentActor(r)
	v, e := s.DB.CreateInvitation(r.Context(), login, "member", digest(raw), time.Now().Add(7*24*time.Hour).UTC().Format(time.RFC3339Nano), a.UserID)
	if e == nil {
		_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "access.invite", "invitation", v.ID, r.Header.Get("X-Request-ID"), map[string]any{"github_login": login})
		result := map[string]any{"invitation": v, "claim_token": raw, "invite_url": strings.TrimRight(s.Origin, "/") + "/?invitation=" + url.QueryEscape(raw)}
		_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "access.invite", result)
		s.ok(w, result, nil)
		return
	}
	s.internal(w, r, e)
}
func (s *Server) handleOwnerClaim(w http.ResponseWriter, r *http.Request) {
	a := currentActor(r)
	if !a.Service {
		s.fail(w, r, 403, "forbidden", "owner claims may only be provisioned by the service", nil)
		return
	}
	raw := token(32)
	claimID, e := s.DB.CreateOwnerClaim(r.Context(), digest(raw), time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339Nano))
	if e != nil {
		s.internal(w, r, e)
		return
	}
	result := map[string]any{"id": claimID, "token": raw, "expires_in": 1800}
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "access.owner_claim", result)
	s.ok(w, result, nil)
}
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.ListDashboardAudit(r.Context(), queryLimit(r))
	s.respond(w, r, v, e)
}

func (s *Server) handleStartPromotion(w http.ResponseWriter, r *http.Request) {
	s.metrics.promotionStarts.Add(1)
	var body map[string]any
	if !s.decode(w, r, &body, 1<<20) {
		return
	}
	a := currentActor(r)
	operation, e := s.DB.CreateHostingOperation(r.Context(), "railway_promotion", r.Header.Get("Idempotency-Key"), a.UserID, body)
	if e != nil {
		s.internal(w, r, e)
		return
	}
	_, _ = s.DB.AppendHostingOperationEvent(r.Context(), operation.ID, "queued", "pending", "Promotion queued", nil)
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "hosting.promote", "hosting_operation", operation.ID, r.Header.Get("X-Request-ID"), nil)
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "hosting.promote", operation)
	if s.Promotion != nil {
		go func() { _ = s.Promotion(context.Background(), operation) }()
	}
	s.okStatus(w, http.StatusAccepted, operation, nil)
}
func (s *Server) handlePromotion(w http.ResponseWriter, r *http.Request) {
	v, e := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, e)
}
func (s *Server) handleResumePromotion(w http.ResponseWriter, r *http.Request) {
	if !s.confirmed(w, r) {
		return
	}
	operation, err := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if operation.Status != "failed" {
		s.fail(w, r, http.StatusConflict, "operation_not_resumable", "only failed promotions may be resumed", nil)
		return
	}
	if err = s.DB.BeginHostingOperation(r.Context(), operation.ID, "prerequisites"); err != nil {
		s.fail(w, r, http.StatusConflict, "operation_not_resumable", err.Error(), nil)
		return
	}
	operation.Status = "running"
	operation.Stage = "prerequisites"
	operation.Attempts++
	a := currentActor(r)
	_, _ = s.DB.AppendHostingOperationEvent(r.Context(), operation.ID, operation.Stage, "pending", "Promotion resume requested", nil)
	_ = s.DB.AppendDashboardAudit(r.Context(), a.UserID, "hosting.resume", "hosting_operation", operation.ID, r.Header.Get("X-Request-ID"), nil)
	_ = s.DB.PutDashboardIdempotency(r.Context(), a.UserID, r.Header.Get("Idempotency-Key"), "hosting.resume", operation)
	if s.Promotion != nil {
		go func() { _ = s.Promotion(context.Background(), operation) }()
	}
	s.okStatus(w, http.StatusAccepted, operation, nil)
}
func (s *Server) handlePromotionStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, r, 500, "stream_unavailable", "streaming unavailable", nil)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if n, e := strconv.ParseInt(raw, 10, 64); e == nil && n > after {
			after = n
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			events, e := s.DB.ListHostingOperationEvents(r.Context(), r.PathValue("id"), after)
			if e != nil {
				return
			}
			for _, event := range events {
				after = event.Seq
				raw, _ := json.Marshal(event)
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Seq, raw)
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			op, e := s.DB.GetHostingOperation(r.Context(), r.PathValue("id"))
			if e == nil && (op.Status == "completed" || op.Status == "failed") {
				raw, _ := json.Marshal(op)
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", raw)
				flusher.Flush()
				return
			}
		}
	}
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
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil && e != io.EOF {
		s.fail(w, r, 400, "invalid_json", e.Error(), nil)
		return false
	}
	return true
}
func (s *Server) respond(w http.ResponseWriter, r *http.Request, v any, e error) {
	if e != nil {
		s.internal(w, r, e)
		return
	}
	s.ok(w, v, nil)
}
func (s *Server) ok(w http.ResponseWriter, v any, meta map[string]any) {
	s.okStatus(w, http.StatusOK, v, meta)
}
func (s *Server) okStatus(w http.ResponseWriter, status int, v any, meta map[string]any) {
	writeJSON(w, status, envelope{Schema: appservice.APISchema, Data: v, Meta: meta})
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request, e error) {
	s.fail(w, r, 500, "internal", redaction.Redact(e.Error()), nil)
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	s.metrics.errors.Add(1)
	writeJSON(w, status, envelope{Schema: appservice.APISchema, Error: &apiError{Code: code, Message: message, RequestID: r.Header.Get("X-Request-ID"), Details: details}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
