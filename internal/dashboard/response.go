package dashboard

import (
	"encoding/json"
	"io"
	"net/http"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
)

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
