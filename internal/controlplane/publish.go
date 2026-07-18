package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

type publishEpicRequest struct {
	Spec              state.EpicSpec `json:"spec"`
	RepositoryID      string         `json:"repository_id"`
	SourceWorkspaceID string         `json:"source_workspace_id,omitempty"`
	SourceEpicID      string         `json:"source_epic_id,omitempty"`
}
type publishedTicket struct {
	VessicaID        string `json:"vessica_id"`
	LinearID         string `json:"linear_id"`
	LinearIdentifier string `json:"linear_identifier"`
	LinearURL        string `json:"linear_url"`
}
type publishEpicResponse struct {
	Epic             *state.Epic       `json:"epic"`
	Tickets          []*state.Ticket   `json:"tickets"`
	LinearID         string            `json:"linear_id"`
	LinearIdentifier string            `json:"linear_identifier"`
	LinearURL        string            `json:"linear_url"`
	LinearTickets    []publishedTicket `json:"linear_tickets"`
}

type startEpicRunRequest struct {
	Runner          string `json:"runner"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Concurrency     int    `json:"concurrency"`
	Preview         *bool  `json:"preview"`
	PRMode          string `json:"pr_mode"`
	StartAt         string `json:"start_at"`
	StopAfter       string `json:"stop_after"`
}

func (s *Server) handleEpics(w http.ResponseWriter, r *http.Request) {
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if repositoryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id required"})
		return
	}
	epics, err := s.DB.ListEpicsForRepository(r.Context(), repositoryID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, epics)
}

func (s *Server) handleEpic(w http.ResponseWriter, r *http.Request) {
	epic, err := s.DB.GetEpic(r.Context(), r.PathValue("epic_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if repositoryID == "" || epic.RepositoryID != repositoryID {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "epic not found in repository"})
		return
	}
	writeJSON(w, http.StatusOK, epic)
}

func (s *Server) handlePublishEpic(w http.ResponseWriter, r *http.Request) {
	var request publishEpicRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Idempotency-Key required"})
		return
	}
	ctx := r.Context()
	var created state.CreatedEpicSpec
	if raw, ok, err := s.DB.GetIdempotency(ctx, "publish:"+key); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	} else if ok {
		if err := json.Unmarshal(raw, &created); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
	} else {
		if strings.TrimSpace(request.RepositoryID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "repository_id required"})
			return
		}
		result, err := s.DB.CreateEpicFromSpecForRepository(ctx, request.RepositoryID, request.Spec)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		created = *result
		if err := s.DB.PutIdempotency(ctx, "publish:"+key, created); err != nil {
			writeJSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
	}
	response, err := s.projectPublishedEpic(ctx, &created)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "epic": created.Epic})
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) handleStartEpicRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	request := startEpicRunRequest{Concurrency: 3, PRMode: "draft"}
	defaultPreview := true
	request.Preview = &defaultPreview
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if request.Concurrency < 1 || request.Concurrency > 16 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "concurrency must be between 1 and 16"})
		return
	}
	if request.PRMode != "none" && request.PRMode != "draft" && request.PRMode != "ready" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pr_mode must be none, draft, or ready"})
		return
	}
	epicID := r.PathValue("epic_id")
	if _, err := s.DB.GetEpic(ctx, epicID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var externalIssue, integrationID string
	if s.Linear != nil && s.Config.Tracker.Provider == "linear" {
		mapping, err := s.DB.GetExternalMapping(ctx, "linear", "epic", epicID)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "epic is not projected to Linear"})
			return
		}
		integration, err := s.DB.GetTrackerIntegration(ctx, "linear")
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}
		externalIssue, integrationID = mapping.ExternalID, integration.ID
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Idempotency-Key required"})
		return
	}
	if raw, ok, _ := s.DB.GetIdempotency(ctx, "run:"+key); ok {
		var existing state.Run
		if json.Unmarshal(raw, &existing) == nil {
			writeJSON(w, http.StatusOK, existing)
			return
		}
	}
	runnerName := request.Runner
	if runnerName == "" {
		runnerName = s.Config.Runner.Default
	}
	model := request.Model
	if model == "" {
		model = s.Config.Runner.Model
	}
	reasoningEffort := request.ReasoningEffort
	if reasoningEffort == "" {
		reasoningEffort = s.Config.Runner.ReasoningEffort
	}
	runRecord, err := s.DB.CreateRun(ctx, epicID, "", runnerName, model, reasoningEffort, "railway", request.Concurrency, *request.Preview, request.PRMode, request.StartAt, request.StopAfter)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	job, err := s.DB.EnqueueJob(ctx, "run_epic", runJobPayload{EpicID: epicID, ExternalIssue: externalIssue, IntegrationID: integrationID}, runRecord.ID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	_ = s.DB.PutIdempotency(ctx, "run:"+key, runRecord)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": runRecord, "job": job})
}

func (s *Server) projectPublishedEpic(ctx context.Context, created *state.CreatedEpicSpec) (*publishEpicResponse, error) {
	if s.Linear == nil || s.Config.Tracker.Provider != "linear" || strings.TrimSpace(s.Config.Tracker.TeamID) == "" || strings.TrimSpace(s.Config.Tracker.TriggerLabel) == "" {
		return &publishEpicResponse{Epic: created.Epic, Tickets: created.Tickets}, nil
	}
	label, err := s.Linear.EnsureIssueLabel(ctx, s.Config.Tracker.TeamID, s.Config.Tracker.TriggerLabel)
	if err != nil {
		return nil, err
	}
	var parent *tracker.LinearIssue
	if mapping, getErr := s.DB.GetExternalMapping(ctx, "linear", "epic", created.Epic.ID); getErr == nil {
		parent, err = s.Linear.GetIssue(ctx, mapping.ExternalID)
	} else {
		parent, err = s.Linear.CreateIssue(ctx, s.Config.Tracker.TeamID, s.Config.Tracker.ProjectID, created.Epic.Title, created.Epic.Body, s.Config.Tracker.TodoStateID, []string{label.ID})
		if err == nil {
			_ = s.DB.SetEpicExternalID(ctx, created.Epic.ID, parent.ID)
			created.Epic.ExternalID = parent.ID
			_, err = s.DB.UpsertExternalMapping(ctx, "linear", "epic", created.Epic.ID, parent.ID, map[string]any{"identifier": parent.Identifier, "url": parent.URL}, "synced")
		}
	}
	if err != nil {
		return nil, err
	}
	response := &publishEpicResponse{Epic: created.Epic, Tickets: created.Tickets, LinearID: parent.ID, LinearIdentifier: parent.Identifier, LinearURL: parent.URL}
	for _, ticket := range created.Tickets {
		var child *tracker.LinearIssue
		if mapping, getErr := s.DB.GetExternalMapping(ctx, "linear", "ticket", ticket.ID); getErr == nil {
			child, err = s.Linear.GetIssue(ctx, mapping.ExternalID)
		} else {
			child, err = s.Linear.CreateSubIssue(ctx, parent, s.Config.Tracker.ProjectID, ticket.Title, ticket.Body, s.Config.Tracker.TodoStateID)
		}
		if err != nil {
			return nil, err
		}
		_, _ = s.DB.UpsertExternalMapping(ctx, "linear", "ticket", ticket.ID, child.ID, map[string]any{"identifier": child.Identifier, "url": child.URL}, "synced")
		response.LinearTickets = append(response.LinearTickets, publishedTicket{VessicaID: ticket.ID, LinearID: child.ID, LinearIdentifier: child.Identifier, LinearURL: child.URL})
	}
	return response, nil
}
