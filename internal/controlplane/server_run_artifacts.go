package controlplane

import (
	"net/http"
	"strings"
)

func (s *Server) handleRunArtifacts(w http.ResponseWriter, r *http.Request) {
	runRecord, err := s.DB.GetRun(r.Context(), r.PathValue("run_id"))
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "run_not_found", err.Error())
		return
	}
	if repositoryID := strings.TrimSpace(r.URL.Query().Get("repository_id")); repositoryID != "" && runRecord.RepositoryID != repositoryID {
		writeAPIError(w, http.StatusNotFound, "run_not_found", "run not found in repository")
		return
	}
	artifacts, err := s.DB.ListArtifactsForRun(r.Context(), runRecord.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "run_artifacts_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, artifacts)
}
