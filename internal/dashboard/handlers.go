package dashboard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
	"github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

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
	repositoryID := strings.TrimSpace(r.Header.Get("X-Vessica-Repository-ID"))
	var v any
	var err error
	if repositoryID != "" {
		v, err = s.App.RunsForRepository(r.Context(), repositoryID, r.URL.Query().Get("cursor"), queryLimit(r))
	} else {
		v, err = s.App.Runs(r.Context(), r.URL.Query().Get("cursor"), queryLimit(r))
	}
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
