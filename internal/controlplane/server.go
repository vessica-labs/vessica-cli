package controlplane

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

type RunLauncher interface {
	Launch(context.Context, *state.Run) error
	Destroy(context.Context, *state.Sandbox) error
}

type Server struct {
	DB                  *state.DB
	Config              config.Config
	Linear              *tracker.LinearClient
	Launcher            RunLauncher
	LinearWebhookSecret string
	APIToken            string
	WorkerDownloadToken string
	BinaryPath          string
	Logger              *log.Logger
	PreviewBroker       *PreviewBroker
	Dashboard           http.Handler
	PreviewPublicURL    string
	PreviewEdgeToken    string
	Credentials         *CredentialManager
	workerID            string
	projectionMu        sync.Mutex
	importMu            sync.Mutex
	mutationMu          sync.Mutex
	binaryDigestOnce    sync.Once
	binaryDigest        string
	binaryDigestErr     error
}

type linearWebhookPayload struct {
	Action           string `json:"action"`
	Type             string `json:"type"`
	WebhookTimestamp int64  `json:"webhookTimestamp"`
	Data             struct {
		ID string `json:"id"`
	} `json:"data"`
}

type runJobPayload struct {
	EpicID        string `json:"epic_id"`
	ExternalIssue string `json:"external_issue_id"`
	IntegrationID string `json:"integration_id"`
	FromPhase     string `json:"from_phase,omitempty"`
}

type repositoryCheckpointRefreshPayload struct {
	RepositoryID string                  `json:"repository_id"`
	SandboxID    string                  `json:"sandbox_id"`
	Checkpoint   reposnapshot.Checkpoint `json:"checkpoint"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /webhooks/linear", s.handleLinearWebhook)
	mux.HandleFunc("GET /internal/worker/ves", s.handleWorkerBinary)
	mux.HandleFunc("GET /api/v1/status", s.requireAPIAuth(s.handleStatus))
	mux.HandleFunc("POST /api/v1/cli-credentials", s.requireServiceAuth(s.handleCreateCLICredential))
	mux.HandleFunc("PUT /api/v1/credentials/{provider}", s.requireAPIAuth(s.handleRotateCredential))
	mux.HandleFunc("GET /api/v1/repositories", s.requireAPIAuth(s.handleRepositories))
	mux.HandleFunc("POST /api/v1/repositories", s.requireAPIAuth(s.handleAttachRepository))
	mux.HandleFunc("PUT /api/v1/repositories/{repository_id}/checkpoint", s.requireAPIAuth(s.handleRepositoryCheckpoint))
	mux.HandleFunc("POST /api/v1/onboarding/operations", s.requireAPIAuth(s.handleUpsertOnboarding))
	mux.HandleFunc("GET /api/v1/onboarding/operations/{operation_id}", s.requireAPIAuth(s.handleOnboarding))
	mux.HandleFunc("POST /api/v1/onboarding/operations/{operation_id}/resume", s.requireAPIAuth(s.handleOnboarding))
	mux.HandleFunc("GET /api/v1/onboarding/operations/{operation_id}/events", s.requireAPIAuth(s.handleOnboarding))
	mux.HandleFunc("GET /api/v1/jobs", s.requireAPIAuth(s.handleJobs))
	mux.HandleFunc("GET /api/v1/runs", s.requireAPIAuth(s.handleRuns))
	mux.HandleFunc("GET /api/v1/runs/{run_id}", s.requireAPIAuth(s.handleRun))
	mux.HandleFunc("GET /api/v1/runs/{run_id}/events", s.requireAPIAuth(s.handleRunEvents))
	mux.HandleFunc("GET /api/v1/runs/{run_id}/artifacts", s.requireAPIAuth(s.handleRunArtifacts))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/resume", s.requireAPIAuth(s.handleResumeRun))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/cancel", s.requireAPIAuth(s.handleCancelRun))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/prompt", s.requireAPIAuth(s.handlePromptRun))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/rollback", s.requireAPIAuth(s.handleRollbackRun))
	mux.HandleFunc("GET /api/v1/receipts/{receipt_id}", s.requireAPIAuth(s.handleReceipt))
	mux.HandleFunc("GET /api/v1/epics", s.requireAPIAuth(s.handleEpics))
	mux.HandleFunc("POST /api/v1/epics", s.requireAPIAuth(s.handlePublishEpic))
	mux.HandleFunc("GET /api/v1/epics/{epic_id}", s.requireAPIAuth(s.handleEpic))
	mux.HandleFunc("GET /api/v1/epics/{epic_id}/status", s.requireAPIAuth(s.handleEpicStatus))
	mux.HandleFunc("POST /api/v1/epics/{epic_id}/runs", s.requireAPIAuth(s.handleStartEpicRun))
	mux.HandleFunc("GET /api/v1/sandboxes", s.requireAPIAuth(s.handleSandboxes))
	mux.HandleFunc("GET /api/v1/sandboxes/{sandbox_id}", s.requireAPIAuth(s.handleSandbox))
	mux.HandleFunc("GET /api/v1/sandboxes/{sandbox_id}/logs", s.requireAPIAuth(s.handleSandboxLogs))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/approve", s.requireAPIAuth(s.handleApproveRun))
	mux.HandleFunc("POST /api/v1/migrations/workplan", s.requireAPIAuth(s.handleImportWorkplan))
	mux.HandleFunc("GET /review/runs/{run_id}", s.handleReviewPage)
	mux.HandleFunc("GET /review/runs/{run_id}/panel", s.handleReviewPage)
	mux.HandleFunc("GET /review/runs/{run_id}/window", s.handleReviewPage)
	mux.HandleFunc("GET /review/runs/{run_id}/events", s.handleReviewEvents)
	mux.HandleFunc("POST /review/runs/{run_id}/prompt", s.handleReviewPrompt)
	mux.HandleFunc("POST /review/runs/{run_id}/approve", s.handleReviewApprove)
	mux.HandleFunc("POST /review/runs/{run_id}/rollback", s.handleReviewRollback)
	if s.PreviewBroker != nil {
		s.PreviewBroker.SetOverlayProvider(s.previewOverlay)
		mux.Handle("/", s.PreviewBroker)
	}
	if s.Dashboard == nil {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.PreviewBroker != nil && constantValue(r.Header.Get(PreviewEdgeHeader), s.PreviewEdgeToken) {
			if previewReviewRoute(r) {
				mux.ServeHTTP(w, r)
				return
			}
			s.PreviewBroker.ServeHTTP(w, r)
			return
		}
		if s.PreviewPublicURL != "" {
			if u, err := url.Parse(s.PreviewPublicURL); err == nil && strings.EqualFold(stripPort(r.Host), stripPort(u.Host)) {
				if previewReviewRoute(r) {
					mux.ServeHTTP(w, r)
					return
				}
				if s.PreviewBroker != nil {
					s.PreviewBroker.ServeHTTP(w, r)
					return
				}
				http.NotFound(w, r)
				return
			}
		}
		if dashboardRoute(r) {
			s.Dashboard.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func previewReviewRoute(r *http.Request) bool {
	return r != nil && strings.HasPrefix(r.URL.Path, "/review/runs/")
}
func (s *Server) handleImportWorkplan(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "read snapshot"})
		return
	}
	var snap state.WorkplanSnapshot
	if err = json.Unmarshal(body, &snap); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid snapshot"})
		return
	}
	if err = s.DB.ImportWorkplanSnapshot(r.Context(), snap); err != nil {
		writeJSON(w, 409, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"imported": true, "checksum": snap.Checksum})
}

func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
func dashboardRoute(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/sandboxes") && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return false
	}
	if strings.HasPrefix(p, "/auth/") || strings.HasPrefix(p, "/assets/") || p == "/internal/dashboard/metrics" {
		return true
	}
	for _, prefix := range []string{"/api/v1/system", "/api/v1/integrations", "/api/v1/sandboxes", "/api/v1/knowledge", "/api/v1/access", "/api/v1/audit", "/api/v1/hosting", "/api/v1/docs"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	if strings.HasPrefix(p, "/api/v1/runs") {
		return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("Accept") == "application/vnd.vessica.dashboard+json"
	}
	if strings.HasPrefix(p, "/api/v1/repositories") {
		return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if strings.HasPrefix(p, "/api/v1/onboarding") {
		return !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	for _, prefix := range []string{"/healthz", "/readyz", "/webhooks/", "/internal/", "/review/", "/previews/", "/api/v1/status", "/api/v1/jobs", "/api/v1/receipts", "/api/v1/epics"} {
		if strings.HasPrefix(p, prefix) {
			return false
		}
	}
	return r.Method == http.MethodGet
}

func (s *Server) Run(ctx context.Context, addr string) error {
	if s.DB == nil {
		return fmt.Errorf("control plane database is required")
	}
	if s.Logger == nil {
		s.Logger = log.New(os.Stdout, "control-plane ", log.LstdFlags|log.LUTC)
	}
	if s.workerID == "" {
		s.workerID = id.New("cpw")
	}
	lease, err := s.DB.AcquireControlPlaneLease(
		ctx,
		"hosted-control-plane",
		s.workerID,
		os.Getenv("RAILWAY_DEPLOYMENT_ID"),
		os.Getenv("RAILWAY_REPLICA_ID"),
		30*time.Second,
	)
	if err != nil {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := lease.Release(releaseCtx); err != nil {
			s.Logger.Printf("release singleton lease: %v", err)
		}
	}()
	if addr == "" {
		addr = ":8080"
	}
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	leaseErrCh := make(chan error, 1)
	go monitorControlPlaneLease(workerCtx, lease, leaseErrCh)
	go s.jobLoop(workerCtx)
	go s.outboxLoop(workerCtx)
	go s.projectionLoop(workerCtx)
	go s.reconciliationLoop(workerCtx)
	go s.sandboxCleanupLoop(workerCtx)

	httpServer := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	s.Logger.Printf("listening on %s", addr)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	var runErr error
	select {
	case <-ctx.Done():
	case <-stop:
	case runErr = <-leaseErrCh:
		if errors.Is(runErr, state.ErrControlPlaneLeaseLost) {
			s.Logger.Printf("singleton lease handed to a new deployment; shutting down")
			runErr = nil
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			runErr = err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return runErr
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "vessica-control-plane"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	result := map[string]any{"ok": true, "installation_id": s.Config.Hosted.ProjectID}
	if workspace, err := s.DB.GetWorkspace(r.Context()); err == nil {
		result["workspace_id"] = workspace.ID
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	integration, _ := s.DB.GetTrackerIntegration(r.Context(), "linear")
	jobs, _ := s.DB.ListJobs(r.Context(), 20)
	workspace, _ := s.DB.GetWorkspace(r.Context())
	repositories, _ := s.DB.ListRepositories(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "workspace": workspace, "repositories": repositories, "integration": integration, "jobs": jobs})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.DB.ListJobs(r.Context(), 100)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "job_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	var runs []state.Run
	var err error
	if repositoryID != "" {
		runs, err = s.DB.ListRunsForRepository(r.Context(), repositoryID)
	} else {
		runs, err = s.DB.ListRuns(r.Context())
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "run_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	runRecord, err := s.DB.GetRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && runRecord.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "run_not_found", "run not found in repository")
		return
	}
	if sandboxRecord, err := s.DB.GetSandboxForRun(r.Context(), runRecord.ID); err == nil {
		runRecord.SandboxID = sandboxRecord.ID
		runRecord.SandboxExpiresAt = sandboxRecord.ExpiresAt
	}
	writeJSON(w, http.StatusOK, runRecord)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	runRecord, err := s.DB.GetRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && runRecord.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "run_not_found", "run not found in repository")
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		_, _ = fmt.Sscan(raw, &after)
	}
	events, err := s.DB.ListEvents(r.Context(), r.PathValue("run_id"), after)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "run_logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleReceipt(w http.ResponseWriter, r *http.Request) {
	record, err := s.DB.GetReceipt(r.Context(), r.PathValue("receipt_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	view, err := receipt.ViewJSON(record)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read webhook"})
		return
	}
	if !tracker.VerifyLinearWebhook(s.LinearWebhookSecret, body, r.Header.Get("Linear-Signature")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid signature"})
		return
	}
	var signedPayload linearWebhookPayload
	if err := json.Unmarshal(body, &signedPayload); err != nil || signedPayload.WebhookTimestamp == 0 || time.Since(time.UnixMilli(signedPayload.WebhookTimestamp)) > time.Minute || time.Until(time.UnixMilli(signedPayload.WebhookTimestamp)) > time.Minute {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "stale or invalid webhook timestamp"})
		return
	}
	integration, err := s.DB.GetTrackerIntegration(r.Context(), "linear")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("Linear-Delivery"))
	if deliveryID == "" {
		deliveryID = fmt.Sprintf("%x", sha256.Sum256(body))
	}
	eventType := strings.TrimSpace(r.Header.Get("Linear-Event"))
	_, _, duplicate, err := s.DB.ReceiveWebhook(r.Context(), integration, deliveryID, eventType, body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "persist webhook"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "duplicate": duplicate})
}

func (s *Server) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !constantToken(r.Header.Get("Authorization"), s.APIToken) && (provided == "" || !s.DB.HasCLICredential(r.Context(), cliTokenHash(provided))) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "API authorization is required")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireServiceAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !constantToken(r.Header.Get("Authorization"), s.APIToken) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "service authorization is required")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireWorkerAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.PreviewBroker == nil {
			writeAPIError(w, http.StatusServiceUnavailable, "preview_broker_unavailable", "preview broker is unavailable")
			return
		}
		if !constantToken(r.Header.Get("Authorization"), s.WorkerDownloadToken) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "worker authorization is required")
			return
		}
		next(w, r)
	}
}

func constantToken(header, token string) bool {
	provided := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	token = strings.TrimSpace(token)
	if provided == "" || token == "" || len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func constantValue(provided, expected string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *Server) handleApproveRun(w http.ResponseWriter, r *http.Request) {
	result, err := s.approveRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
