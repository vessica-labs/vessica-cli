package controlplane

import (
	"encoding/json"
	"net/http"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type hostedOnboardingRequest struct {
	ID           string          `json:"id"`
	RepositoryID string          `json:"repository_id"`
	Status       string          `json:"status"`
	CurrentStage string          `json:"current_stage"`
	Document     json.RawMessage `json:"document"`
}

func (s *Server) handleUpsertOnboarding(w http.ResponseWriter, r *http.Request) {
	var request hostedOnboardingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid onboarding operation"})
		return
	}
	operation, err := s.DB.UpsertOnboardingOperation(r.Context(), state.OnboardingOperation{ID: request.ID, RepositoryID: request.RepositoryID, Status: request.Status, CurrentStage: request.CurrentStage, DocumentJSON: string(request.Document)})
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	operation, err := s.DB.GetOnboardingOperation(r.Context(), r.PathValue("operation_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var document any
	if err := json.Unmarshal([]byte(operation.DocumentJSON), &document); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, document)
}
