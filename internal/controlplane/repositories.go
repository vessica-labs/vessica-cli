package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type attachRepositoryRequest struct {
	Remote string `json:"remote"`
}

func (s *Server) handleRepositoryCheckpoint(w http.ResponseWriter, r *http.Request) {
	repository, err := s.DB.GetRepository(r.Context(), r.PathValue("repository_id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	var checkpoint reposnapshot.Checkpoint
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&checkpoint); err != nil || checkpoint.SchemaVersion != reposnapshot.SchemaVersion || checkpoint.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "valid repository checkpoint required"})
		return
	}
	metadata, err := reposnapshot.Merge(repository.MetadataJSON, checkpoint)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	updated, err := s.DB.UpdateRepositoryMetadata(r.Context(), repository.ID, metadata)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	repositories, err := s.DB.ListRepositories(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": repositories})
}

func (s *Server) handleAttachRepository(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Idempotency-Key required"})
		return
	}
	var request attachRepositoryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil || state.CanonicalRepositoryRemote(request.Remote) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "valid repository remote required"})
		return
	}
	var repository state.Repository
	if raw, ok, err := s.DB.GetIdempotency(r.Context(), "repository:"+key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	} else if ok {
		if err := json.Unmarshal(raw, &repository); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, repository)
		return
	}
	workspace, err := s.DB.GetWorkspace(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	created, err := s.DB.EnsureRepository(r.Context(), workspace.ID, request.Remote)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	repository = *created
	if err := s.DB.PutIdempotency(r.Context(), "repository:"+key, repository); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, repository)
}
