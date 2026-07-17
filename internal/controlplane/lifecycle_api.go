package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type resumeRunRequest struct {
	From string `json:"from,omitempty"`
}

type runMutationResponse struct {
	Run *state.Run `json:"run"`
	Job *state.Job `json:"job,omitempty"`
}

func (s *Server) handleRotateCredential(w http.ResponseWriter, r *http.Request) {
	if s.Credentials == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "credential_rotation_unavailable", "hosted credential storage is not configured")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	if provider != "linear" && provider != "railway" {
		writeAPIError(w, http.StatusBadRequest, "unsupported_provider", "provider must be linear or railway")
		return
	}
	var request struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&request); err != nil || strings.TrimSpace(request.Credential) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_credential", "OAuth credential payload is required")
		return
	}
	if err := s.Credentials.Rotate(r.Context(), provider, request.Credential); err != nil {
		writeAPIError(w, http.StatusUnauthorized, "credential_validation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider, "rotated": true})
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key header is required")
		return
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	var request resumeRunRequest
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "resume request is invalid")
			return
		}
	}
	request.From = strings.TrimSpace(request.From)
	if request.From != "" && !knownRunPhase(request.From) {
		writeAPIError(w, http.StatusBadRequest, "invalid_phase", "unknown resume phase: "+request.From)
		return
	}
	ctx := r.Context()
	runRecord, err := s.DB.GetRun(ctx, r.PathValue("run_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && runRecord.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "run_not_found", "run not found in repository")
		return
	}
	idempotencyKey := "resume:" + r.PathValue("run_id") + ":" + key
	if raw, ok, err := s.DB.GetIdempotency(ctx, idempotencyKey); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "idempotency_read_failed", err.Error())
		return
	} else if ok {
		var response runMutationResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "idempotency_decode_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	switch runRecord.Status {
	case "running", "pending":
		writeAPIError(w, http.StatusConflict, "run_active", "run is already active")
		return
	}
	sandboxRecord, err := s.DB.GetSandboxForRun(ctx, runRecord.ID)
	if err != nil || sandboxRecord.Backend != "railway" || sandboxRecord.ContainerID == "" || sandboxRecord.Status == "destroyed" || sandboxRecord.Status == "expired" {
		writeAPIError(w, http.StatusConflict, "retained_sandbox_unavailable", "the retained Railway sandbox is unavailable")
		return
	}
	from := request.From
	if from == "" {
		from = runRecord.CurrentPhase
	}
	if from == "" || !knownRunPhase(from) {
		writeAPIError(w, http.StatusConflict, "resume_phase_unavailable", "the run has no resumable phase")
		return
	}
	if _, err := s.DB.CancelJobsForRun(ctx, runRecord.ID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "job_release_failed", err.Error())
		return
	}
	runRecord.Status = "pending"
	runRecord.CurrentPhase = from
	runRecord.Error = ""
	runRecord.FinishedAt = ""
	if err := s.DB.UpdateRun(ctx, runRecord); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "run_update_failed", err.Error())
		return
	}
	payload := s.runPayload(ctx, runRecord, from)
	job, err := s.DB.EnqueueJob(ctx, "run_epic", payload, runRecord.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "resume_enqueue_failed", err.Error())
		return
	}
	response := runMutationResponse{Run: runRecord, Job: job}
	if err := s.DB.PutIdempotency(ctx, idempotencyKey, response); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "idempotency_write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAPIError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key header is required")
		return
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	ctx := r.Context()
	if runRecord, err := s.DB.GetRun(ctx, r.PathValue("run_id")); err != nil {
		writeAPIError(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	} else if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && runRecord.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "run_not_found", "run not found in repository")
		return
	}
	idempotencyKey := "cancel:" + r.PathValue("run_id") + ":" + key
	if raw, ok, err := s.DB.GetIdempotency(ctx, idempotencyKey); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "idempotency_read_failed", err.Error())
		return
	} else if ok {
		var response runMutationResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "idempotency_decode_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	runRecord, err := s.runLifecycle().Cancel(ctx, r.PathValue("run_id"), "hosted_api")
	if err != nil {
		writeAPIError(w, http.StatusConflict, "cancel_failed", err.Error())
		return
	}
	if canceller, ok := s.Launcher.(interface{ CancelRun(string) }); ok {
		canceller.CancelRun(runRecord.ID)
	}
	response := runMutationResponse{Run: runRecord}
	if err := s.DB.PutIdempotency(ctx, idempotencyKey, response); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "idempotency_write_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleEpicStatus(w http.ResponseWriter, r *http.Request) {
	epic, err := s.DB.GetEpic(r.Context(), r.PathValue("epic_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "epic_not_found", err.Error())
		return
	}
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if repositoryID == "" || epic.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "epic_not_found", "epic not found in repository")
		return
	}
	tickets, err := s.DB.ListTickets(r.Context(), epic.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "ticket_list_failed", err.Error())
		return
	}
	ready, err := s.DB.ReadyTickets(r.Context(), epic.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "ticket_status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"epic": epic, "tickets": len(tickets), "ready": len(ready)})
}

func (s *Server) handleSandboxes(w http.ResponseWriter, r *http.Request) {
	records, err := s.DB.ListSandboxes(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "sandbox_list_failed", err.Error())
		return
	}
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if repositoryID != "" {
		filtered := records[:0]
		for i := range records {
			if s.sandboxInRepository(r.Context(), &records[i], repositoryID) {
				filtered = append(filtered, records[i])
			}
		}
		records = filtered
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) handleSandbox(w http.ResponseWriter, r *http.Request) {
	record, err := s.DB.GetSandbox(r.Context(), r.PathValue("sandbox_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "sandbox_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && !s.sandboxInRepository(r.Context(), record, repositoryID) {
		writeAPIError(w, http.StatusNotFound, "sandbox_not_found", "sandbox not found in repository")
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) handleSandboxLogs(w http.ResponseWriter, r *http.Request) {
	record, err := s.DB.GetSandbox(r.Context(), r.PathValue("sandbox_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "sandbox_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && !s.sandboxInRepository(r.Context(), record, repositoryID) {
		writeAPIError(w, http.StatusNotFound, "sandbox_not_found", "sandbox not found in repository")
		return
	}
	inspector, ok := s.Launcher.(interface {
		SandboxLogs(context.Context, *state.Sandbox) (string, error)
	})
	if !ok {
		writeAPIError(w, http.StatusNotImplemented, "sandbox_logs_unavailable", "sandbox logs are unavailable for this launcher")
		return
	}
	logs, err := inspector.SandboxLogs(r.Context(), record)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "sandbox_logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sandbox_id": record.ID, "logs": logs})
}

func (s *Server) sandboxInRepository(ctx context.Context, record *state.Sandbox, repositoryID string) bool {
	if record == nil || record.RunID == "" {
		return false
	}
	runRecord, err := s.DB.GetRun(ctx, record.RunID)
	return err == nil && runRecord.RepositoryID == repositoryID
}

func (s *Server) runPayload(ctx context.Context, runRecord *state.Run, from string) runJobPayload {
	payload := runJobPayload{EpicID: runRecord.EpicID, FromPhase: from}
	if mapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", runRecord.EpicID); err == nil {
		payload.ExternalIssue = mapping.ExternalID
	}
	if integration, err := s.DB.GetTrackerIntegration(ctx, "linear"); err == nil {
		payload.IntegrationID = integration.ID
	}
	return payload
}

func knownRunPhase(phase string) bool {
	for _, candidate := range state.SoftwareEpicPhases {
		if candidate == phase {
			return true
		}
	}
	return false
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
