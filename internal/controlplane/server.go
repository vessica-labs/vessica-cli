package controlplane

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
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
	workerID            string
	projectionMu        sync.Mutex
	importMu            sync.Mutex
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
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /webhooks/linear", s.handleLinearWebhook)
	mux.HandleFunc("GET /internal/worker/ves", s.handleWorkerBinary)
	mux.HandleFunc("GET /api/v1/status", s.requireAPIAuth(s.handleStatus))
	mux.HandleFunc("GET /api/v1/jobs", s.requireAPIAuth(s.handleJobs))
	mux.HandleFunc("GET /api/v1/runs", s.requireAPIAuth(s.handleRuns))
	mux.HandleFunc("GET /api/v1/runs/{run_id}", s.requireAPIAuth(s.handleRun))
	mux.HandleFunc("GET /api/v1/runs/{run_id}/events", s.requireAPIAuth(s.handleRunEvents))
	mux.HandleFunc("GET /api/v1/receipts/{receipt_id}", s.requireAPIAuth(s.handleReceipt))
	mux.HandleFunc("POST /api/v1/epics", s.requireAPIAuth(s.handlePublishEpic))
	mux.HandleFunc("POST /api/v1/epics/{epic_id}/runs", s.requireAPIAuth(s.handleStartEpicRun))
	mux.HandleFunc("POST /api/v1/runs/{run_id}/approve", s.requireAPIAuth(s.handleApproveRun))
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
	return mux
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
	if addr == "" {
		addr = ":8080"
	}
	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
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
	select {
	case <-ctx.Done():
	case <-stop:
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "vessica-control-plane"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	integration, _ := s.DB.GetTrackerIntegration(r.Context(), "linear")
	jobs, _ := s.DB.ListJobs(r.Context(), 20)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "integration": integration, "jobs": jobs})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.DB.ListJobs(r.Context(), 100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.DB.ListRuns(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	runRecord, err := s.DB.GetRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	if sandboxRecord, err := s.DB.GetSandboxForRun(r.Context(), runRecord.ID); err == nil {
		runRecord.SandboxID = sandboxRecord.ID
		runRecord.SandboxExpiresAt = sandboxRecord.ExpiresAt
	}
	writeJSON(w, http.StatusOK, runRecord)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		_, _ = fmt.Sscan(raw, &after)
	}
	events, err := s.DB.ListEvents(r.Context(), r.PathValue("run_id"), after)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
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

func (s *Server) handleWorkerBinary(w http.ResponseWriter, r *http.Request) {
	if !constantToken(r.Header.Get("Authorization"), s.WorkerDownloadToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	path := s.BinaryPath
	if path == "" {
		path = "/proc/self/exe"
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "worker binary unavailable"})
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="ves"`)
	_, _ = io.Copy(w, file)
}

func (s *Server) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !constantToken(r.Header.Get("Authorization"), s.APIToken) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
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

func (s *Server) jobLoop(ctx context.Context) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := s.DB.ClaimJob(ctx, s.workerID, 6*time.Hour)
			if err != nil || job == nil {
				continue
			}
			if err := s.processJob(ctx, job); err != nil {
				s.Logger.Printf("job %s failed: %v", job.ID, err)
				_ = s.DB.FailJob(ctx, job, err.Error())
			} else {
				_ = s.DB.CompleteJob(ctx, job.ID)
			}
		}
	}
}

func (s *Server) processJob(ctx context.Context, job *state.Job) error {
	switch job.Kind {
	case "tracker_webhook":
		var payload struct {
			DeliveryID string `json:"delivery_id"`
		}
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.processLinearDelivery(ctx, payload.DeliveryID)
	case "run_epic":
		return s.processRunEpic(ctx, job)
	case "sync_run":
		var payload struct {
			RunID string `json:"run_id"`
		}
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
			return err
		}
		return s.SyncRunToLinear(ctx, payload.RunID)
	default:
		return fmt.Errorf("unsupported job kind %s", job.Kind)
	}
}

func (s *Server) processLinearDelivery(ctx context.Context, deliveryID string) error {
	delivery, err := s.DB.GetWebhookDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	var payload linearWebhookPayload
	if err := json.Unmarshal([]byte(delivery.PayloadJSON), &payload); err != nil {
		_ = s.DB.FailWebhookDelivery(ctx, delivery.ID, err.Error())
		return err
	}
	if !strings.EqualFold(payload.Type, "Issue") || (payload.Action != "create" && payload.Action != "update") || payload.Data.ID == "" {
		return s.DB.CompleteWebhookDelivery(ctx, delivery.ID)
	}
	if err := s.ImportLinearIssue(ctx, payload.Data.ID); err != nil {
		_ = s.DB.FailWebhookDelivery(ctx, delivery.ID, err.Error())
		return err
	}
	return s.DB.CompleteWebhookDelivery(ctx, delivery.ID)
}

func (s *Server) ImportLinearIssue(ctx context.Context, issueID string) error {
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if s.Linear == nil {
		return fmt.Errorf("Linear client is not configured")
	}
	issue, err := s.Linear.GetIssue(ctx, issueID)
	if err != nil {
		return err
	}
	if issue.Parent != nil || issue.Team.ID != s.Config.Tracker.TeamID || issue.State.ID != s.Config.Tracker.TodoStateID || !tracker.LinearIssueHasLabel(issue, s.Config.Tracker.TriggerLabel) {
		return nil
	}
	if _, err := s.DB.GetExternalMappingByExternal(ctx, "linear", "epic", issue.ID); err == nil {
		return nil
	}
	integration, err := s.DB.GetTrackerIntegration(ctx, "linear")
	if err != nil {
		return err
	}
	epic, err := s.DB.CreateEpic(ctx, issue.Title, issue.Description)
	if err != nil {
		return err
	}
	_ = s.DB.SetEpicExternalID(ctx, epic.ID, issue.ID)
	if _, err := s.DB.UpsertExternalMapping(ctx, "linear", "epic", epic.ID, issue.ID, map[string]any{"identifier": issue.Identifier, "url": issue.URL}, "synced"); err != nil {
		return err
	}
	_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.issue_state", "linear:epic:wip:"+epic.ID, map[string]any{"issue_id": issue.ID, "state_id": s.Config.Tracker.WIPStateID})
	_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", "linear:epic:accepted:"+epic.ID, map[string]any{
		"issue_id": issue.ID, "entity_type": "epic_comment", "local_id": epic.ID,
		"body": "<!-- vessica:epic:" + epic.ID + " -->\nVessica accepted this epic and is starting planning.",
	})
	_, err = s.DB.EnqueueJob(ctx, "run_epic", runJobPayload{EpicID: epic.ID, ExternalIssue: issue.ID, IntegrationID: integration.ID}, "")
	return err
}

func (s *Server) processRunEpic(ctx context.Context, job *state.Job) error {
	if s.Launcher == nil {
		return fmt.Errorf("run launcher is not configured")
	}
	var payload runJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return err
	}
	var runRecord *state.Run
	var err error
	if job.RunID != "" {
		runRecord, err = s.DB.GetRun(ctx, job.RunID)
	} else {
		runRecord, err = s.DB.CreateRun(ctx, payload.EpicID, "", s.Config.Runner.Default, s.Config.Runner.Model, s.Config.Runner.ReasoningEffort, "railway", 1, true, "draft", "", "")
		if err == nil {
			_ = s.DB.SetJobRunID(ctx, job.ID, runRecord.ID)
			job.RunID = runRecord.ID
		}
	}
	if err != nil {
		return err
	}
	if runRecord.Status == "completed" {
		return s.SyncRunToLinear(ctx, runRecord.ID)
	}
	if err := s.Launcher.Launch(ctx, runRecord); err != nil {
		if job.Attempts >= job.MaxAttempts && s.Config.Tracker.BlockedStateID != "" {
			_, _ = s.DB.EnqueueOutbox(ctx, payload.IntegrationID, "linear.issue_state", "linear:epic:blocked:"+runRecord.ID, map[string]any{"issue_id": payload.ExternalIssue, "state_id": s.Config.Tracker.BlockedStateID})
		}
		if job.Attempts >= job.MaxAttempts {
			_, _ = s.DB.EnqueueOutbox(ctx, payload.IntegrationID, "linear.comment", "linear:run:failed:"+runRecord.ID, map[string]any{"issue_id": payload.ExternalIssue, "entity_type": "run_comment", "local_id": runRecord.ID, "body": "<!-- vessica:run:" + runRecord.ID + " -->\nVessica run failed: `" + err.Error() + "`"})
		}
		return err
	}
	return s.SyncRunToLinear(ctx, runRecord.ID)
}

func (s *Server) SyncRunToLinear(ctx context.Context, runID string) error {
	s.projectionMu.Lock()
	defer s.projectionMu.Unlock()
	runRecord, err := s.DB.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	epicMapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", runRecord.EpicID)
	if err != nil {
		return err
	}
	integration, err := s.DB.GetTrackerIntegration(ctx, "linear")
	if err != nil {
		return err
	}
	artifacts, _ := s.DB.ListArtifactsForRun(ctx, runID)
	for _, artifact := range artifacts {
		body := fmt.Sprintf("<!-- vessica:artifact:%s:v%d -->\n## %s\n\n%s", artifact.ID, artifact.Version, artifact.Title, artifact.Body)
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", fmt.Sprintf("linear:artifact:%s:v%d", artifact.ID, artifact.Version), map[string]any{"issue_id": epicMapping.ExternalID, "entity_type": "artifact_comment", "local_id": artifact.ID, "body": body})
	}
	tickets, _ := s.DB.ListTicketsForRun(ctx, runRecord.EpicID, runID)
	for _, ticket := range tickets {
		stateID := s.Config.Tracker.TodoStateID
		if ticket.Status == "claimed" || ticket.Status == "in_progress" {
			stateID = s.Config.Tracker.WIPStateID
		} else if ticket.Status == "closed" {
			stateID = s.Config.Tracker.DoneStateID
		} else if ticket.Status == "blocked" && s.Config.Tracker.BlockedStateID != "" {
			stateID = s.Config.Tracker.BlockedStateID
		}
		key := fmt.Sprintf("linear:ticket:%s:%s", ticket.ID, ticket.Status)
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.subissue", key, map[string]any{"parent_id": epicMapping.ExternalID, "ticket_id": ticket.ID, "title": ticket.Title, "description": ticket.Body, "state_id": stateID})
	}
	if runRecord.Status == "completed" {
		body := fmt.Sprintf("<!-- vessica:run:%s -->\nVessica completed the run.\n\n- Preview: %s\n- Draft PR: %s\n- Receipt: `%s`", runID, runRecord.PreviewURL, runRecord.PRURL, runRecord.ReceiptID)
		acceptURL, rollbackURL := s.reviewURL(runID, "approve"), s.reviewURL(runID, "rollback")
		if acceptURL != "" && rollbackURL != "" && runRecord.PRMode != "merged" && runRecord.PRMode != "rolled_back" {
			body += fmt.Sprintf("\n\n**[Accept and Merge](%s)** · [Rollback](%s)", acceptURL, rollbackURL)
		}
		_, _ = s.DB.EnqueueOutbox(ctx, integration.ID, "linear.comment", "linear:run:completed:v2:"+runID, map[string]any{"issue_id": epicMapping.ExternalID, "entity_type": "run_comment", "local_id": runID, "body": body})
	}
	if runTerminalStatus(runRecord.Status) {
		return s.recordTerminalRunKnowledge(ctx, runRecord, epicMapping.ExternalID)
	}
	return nil
}

func (s *Server) recordTerminalRunKnowledge(ctx context.Context, runRecord *state.Run, linearIssueID string) error {
	if s.Config.Knowledge.Mode != "hosted" || s.Config.Knowledge.Endpoint == "" {
		return nil
	}
	localKey := runRecord.ID + ":" + runRecord.Status
	if _, err := s.DB.GetExternalMapping(ctx, "knowledge", "run_episode", localKey); err == nil {
		return nil
	}
	gateway, err := knowledgegateway.Open(".", s.Config, s.Config.Knowledge.WorkspaceID)
	if err != nil {
		return err
	}
	defer gateway.Close()
	scope, err := gateway.EnsureRepositoryScope(ctx, knowledgegateway.CanonicalRepository(s.Config.Repo.Remote, "."), s.Config.Repo.Remote)
	if err != nil {
		return err
	}
	eventType := "run." + runRecord.Status
	summary := "Run " + runRecord.Status
	if epic, epicErr := s.DB.GetEpic(ctx, runRecord.EpicID); epicErr == nil && epic.Title != "" {
		summary += ": " + epic.Title
	}
	refs := []knowledge.ExternalRef{{System: "vessica.epic", ID: runRecord.EpicID}, {System: "vessica.run", ID: runRecord.ID}}
	if linearIssueID != "" {
		refs = append(refs, knowledge.ExternalRef{System: "linear.issue", ID: linearIssueID})
	}
	if runRecord.ReceiptID != "" {
		refs = append(refs, knowledge.ExternalRef{System: "vessica.receipt", ID: runRecord.ReceiptID})
	}
	if runRecord.PRURL != "" {
		refs = append(refs, knowledge.ExternalRef{System: "github.pull_request", ID: runRecord.PRURL, URL: runRecord.PRURL})
	}
	event := knowledge.WorkflowEvent{ID: "run:" + runRecord.ID + ":" + runRecord.Status, RepositoryScopeID: scope.ID, Type: eventType, Summary: summary, OccurredAt: time.Now().UTC(), Actor: knowledge.Actor{ID: "vessica-control-plane", Type: "service"}, References: refs, Metadata: map[string]any{"run_status": runRecord.Status, "phase": runRecord.CurrentPhase}}
	memory, err := gateway.Workflow(ctx, event.ID, event)
	if err != nil {
		return err
	}
	_, err = s.DB.UpsertExternalMapping(ctx, "knowledge", "run_episode", localKey, memory.ID, map[string]any{"workflow_event_id": event.ID}, "synced")
	return err
}

func runTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "stopped":
		return true
	default:
		return false
	}
}

func (s *Server) outboxLoop(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			message, err := s.DB.ClaimOutbox(ctx)
			if err != nil || message == nil {
				continue
			}
			if err := s.processOutbox(ctx, message); err != nil {
				_ = s.DB.FailOutbox(ctx, message, err.Error())
			} else {
				_ = s.DB.CompleteOutbox(ctx, message.ID)
			}
		}
	}
}

func (s *Server) processOutbox(ctx context.Context, message *state.OutboxMessage) error {
	if message.Operation == "knowledge.workflow_event" || message.Operation == "knowledge.artifact" {
		gateway, err := knowledgegateway.Open(".", s.Config, message.WorkspaceID)
		if err != nil {
			return err
		}
		defer gateway.Close()
		switch message.Operation {
		case "knowledge.workflow_event":
			var v knowledge.WorkflowEvent
			if err := json.Unmarshal([]byte(message.PayloadJSON), &v); err != nil {
				return err
			}
			_, err = gateway.Workflow(ctx, message.IdempotencyKey, v)
			return err
		case "knowledge.artifact":
			var v knowledge.Artifact
			if err := json.Unmarshal([]byte(message.PayloadJSON), &v); err != nil {
				return err
			}
			_, err = gateway.CreateArtifact(ctx, message.IdempotencyKey, v)
			return err
		}
	}
	if s.Linear == nil {
		return fmt.Errorf("Linear client is not configured")
	}
	var payload struct {
		IssueID     string `json:"issue_id"`
		StateID     string `json:"state_id"`
		EntityType  string `json:"entity_type"`
		LocalID     string `json:"local_id"`
		Body        string `json:"body"`
		ParentID    string `json:"parent_id"`
		TicketID    string `json:"ticket_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil {
		return err
	}
	switch message.Operation {
	case "linear.issue_state":
		return s.Linear.UpdateIssueState(ctx, payload.IssueID, payload.StateID)
	case "linear.comment":
		mapping, err := s.DB.GetExternalMapping(ctx, "linear", payload.EntityType, payload.LocalID)
		if err == nil {
			return s.Linear.UpdateComment(ctx, mapping.ExternalID, payload.Body)
		}
		comment, err := s.Linear.CreateComment(ctx, payload.IssueID, payload.Body)
		if err != nil {
			return err
		}
		_, err = s.DB.UpsertExternalMapping(ctx, "linear", payload.EntityType, payload.LocalID, comment.ID, nil, "synced")
		return err
	case "linear.subissue":
		mapping, err := s.DB.GetExternalMapping(ctx, "linear", "ticket", payload.TicketID)
		if err == nil {
			return s.Linear.UpdateIssueState(ctx, mapping.ExternalID, payload.StateID)
		}
		parent, err := s.Linear.GetIssue(ctx, payload.ParentID)
		if err != nil {
			return err
		}
		child, err := s.Linear.CreateSubIssue(ctx, parent, payload.Title, payload.Description, payload.StateID)
		if err != nil {
			return err
		}
		_, err = s.DB.UpsertExternalMapping(ctx, "linear", "ticket", payload.TicketID, child.ID, map[string]any{"identifier": child.Identifier, "url": child.URL}, "synced")
		return err
	default:
		return fmt.Errorf("unsupported outbox operation %s", message.Operation)
	}
}

func (s *Server) projectionLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runs, err := s.DB.ListRuns(ctx)
			if err != nil {
				continue
			}
			for i := range runs {
				if runs[i].Status == "running" || runs[i].Status == "completed" || runs[i].Status == "failed" {
					_ = s.SyncRunToLinear(ctx, runs[i].ID)
				}
			}
		}
	}
}

func (s *Server) reconciliationLoop(ctx context.Context) {
	_ = s.ReconcileLinear(ctx)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.ReconcileLinear(ctx)
		}
	}
}

func (s *Server) ReconcileLinear(ctx context.Context) error {
	if s.Linear == nil || s.Config.Tracker.TeamID == "" || s.Config.Tracker.TodoStateID == "" {
		return nil
	}
	issues, err := s.Linear.ListIssuesInState(ctx, s.Config.Tracker.TeamID, s.Config.Tracker.TodoStateID)
	if err != nil {
		_ = s.DB.SetTrackerIntegrationSync(ctx, "linear", "error", err.Error())
		return err
	}
	for i := range issues {
		_ = s.ImportLinearIssue(ctx, issues[i].ID)
	}
	return s.DB.SetTrackerIntegrationSync(ctx, "linear", "connected", "")
}

func (s *Server) sandboxCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			records, err := s.DB.ListSandboxes(ctx)
			if err != nil {
				continue
			}
			for i := range records {
				record := &records[i]
				if record.Backend != "railway" || record.Status == "destroyed" || record.Status == "expired" {
					continue
				}
				expiry := retention.EffectiveExpiry(record)
				if !expiry.IsZero() && time.Now().After(expiry) && s.Launcher != nil {
					if err := s.Launcher.Destroy(ctx, record); err == nil {
						record.Status = "expired"
						_ = s.DB.UpdateSandbox(ctx, record)
					}
				}
			}
		}
	}
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
