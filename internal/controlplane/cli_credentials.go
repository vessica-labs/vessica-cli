package controlplane

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (s *Server) handleCreateCLICredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil || strings.TrimSpace(body.Subject) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "credential subject is required"})
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "credential generation failed"})
		return
	}
	token := "ves_" + base64.RawURLEncoding.EncodeToString(raw)
	if err := s.DB.CreateCLICredential(r.Context(), body.Subject, cliTokenHash(token)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "subject": strings.TrimSpace(body.Subject), "scope": "workspace"})
}

func cliTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
