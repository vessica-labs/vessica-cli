package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func (s *Server) requireIdempotency(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			writeAPIError(w, http.StatusBadRequest, "idempotency_required", "Idempotency-Key header is required")
			return
		}
		key = r.Method + ":" + r.URL.Path + ":" + key
		if raw, ok, err := s.DB.GetIdempotency(r.Context(), key); err == nil && ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
			return
		}
		rec := httptest.NewRecorder()
		next(rec, r)
		for k, values := range rec.Header() {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		if rec.Code >= 200 && rec.Code < 300 {
			_ = s.DB.PutIdempotency(r.Context(), key, json.RawMessage(rec.Body.Bytes()))
		}
	}
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	v, err := s.agentApp().Agents(r.Context())
	if err != nil {
		writeAPIError(w, 500, "internal", redaction.Redact(err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"agents": v})
}
func (s *Server) handleAgentBuilds(w http.ResponseWriter, r *http.Request) {
	builds, err := s.DB.ListAgentBuilds(r.Context())
	if err != nil {
		writeAPIError(w, 500, "internal", redaction.Redact(err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"builds": builds})
}
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	v, err := s.agentApp().Agent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, 404, "not_found", err.Error())
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var d generalagent.Definition
	if err := decodeAgentJSON(w, r, &d); err != nil {
		return
	}
	v, err := s.agentApp().CreateStructuredAgent(r.Context(), d, map[string]any{"source": "api"})
	if err != nil {
		writeAPIError(w, 400, "invalid_agent", err.Error())
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	var d generalagent.Definition
	if err := decodeAgentJSON(w, r, &d); err != nil {
		return
	}
	v, err := s.agentApp().UpdateStructuredAgent(r.Context(), r.PathValue("id"), d)
	if err != nil {
		writeAPIError(w, 400, "invalid_agent", err.Error())
		return
	}
	writeJSON(w, 200, v)
}
func decodeAgentJSON(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeAPIError(w, 400, "invalid_json", err.Error())
		return err
	}
	return nil
}

func (s *Server) handleCreateAgentBuild(w http.ResponseWriter, r *http.Request) {
	if !s.agentRuntimeReady() {
		writeAPIError(w, http.StatusServiceUnavailable, "agent_runtime_credentials_required", "configure the agent runtime with ves auth login openai --env OPENAI_API_KEY")
		return
	}
	var body struct {
		Description string `json:"description"`
		AgentID     string `json:"agent_id,omitempty"`
		Review      bool   `json:"review"`
		Timezone    string `json:"timezone,omitempty"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	op, err := s.agentApp().CreateAgentBuild(r.Context(), body.Description, body.AgentID, "cli", body.Review, body.Timezone)
	if err != nil {
		writeAPIError(w, 400, "invalid_build", err.Error())
		return
	}
	writeJSON(w, 202, op)
}
func (s *Server) handleAgentBuild(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	if strings.HasPrefix(buildID, "adraft_") {
		_ = s.DB.QueryRow(r.Context(), `SELECT operation_id FROM agent_drafts WHERE id=?`, buildID).Scan(&buildID)
	}
	op, err := s.DB.GetAgentBuild(r.Context(), buildID)
	if err != nil {
		writeAPIError(w, 404, "not_found", err.Error())
		return
	}
	result := map[string]any{"operation": op}
	var draftID string
	if err = s.DB.QueryRow(r.Context(), `SELECT id FROM agent_drafts WHERE operation_id=?`, op.ID).Scan(&draftID); err == nil {
		draft, _ := s.DB.GetAgentDraft(r.Context(), draftID)
		result["draft"] = draft
	}
	writeJSON(w, 200, result)
}
func (s *Server) handleActivateAgentBuild(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DraftID string `json:"draft_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DraftID == "" && strings.HasPrefix(r.PathValue("id"), "adraft_") {
		body.DraftID = r.PathValue("id")
	}
	if body.DraftID == "" {
		_ = s.DB.QueryRow(r.Context(), `SELECT id FROM agent_drafts WHERE operation_id=?`, r.PathValue("id")).Scan(&body.DraftID)
	}
	a, err := s.agentApp().ActivateAgentDraft(r.Context(), body.DraftID)
	if err != nil {
		writeAPIError(w, 409, "activation_failed", err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) handleAgentState(w http.ResponseWriter, r *http.Request) {
	a, err := s.DB.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, 404, "not_found", err.Error())
		return
	}
	state := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/"+r.PathValue("id")+"/")
	if state == "resume" {
		state = "active"
	} else if state == "pause" {
		state = "paused"
	}
	if err = s.DB.SetAgentState(r.Context(), a.ID, state); err != nil {
		writeAPIError(w, 409, "state_change_failed", err.Error())
		return
	}
	a, _ = s.DB.GetAgent(r.Context(), a.ID)
	writeJSON(w, 200, a)
}
func (s *Server) handleAgentBudget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DailyUSD string `json:"daily_usd"`
		Timezone string `json:"timezone"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	released, err := s.agentApp().SetAgentBudget(r.Context(), r.PathValue("id"), body.DailyUSD, body.Timezone)
	if err != nil {
		writeAPIError(w, 400, "invalid_budget", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true, "released_runs": released})
}
func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		a, err := s.DB.GetAgent(r.Context(), r.PathValue("id"))
		if err == nil {
			err = s.DB.DisableAgentSchedule(r.Context(), a.ID)
		}
		if err != nil {
			writeAPIError(w, 400, "heartbeat_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"disabled": true})
		return
	}
	var h generalagent.Heartbeat
	if err := decodeAgentJSON(w, r, &h); err != nil {
		return
	}
	h.Enabled = true
	if err := s.agentApp().SetAgentHeartbeat(r.Context(), r.PathValue("id"), h); err != nil {
		writeAPIError(w, 400, "invalid_heartbeat", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func (s *Server) handleStartAgentRun(w http.ResponseWriter, r *http.Request) {
	if !s.agentRuntimeReady() {
		writeAPIError(w, http.StatusServiceUnavailable, "agent_runtime_credentials_required", "configure the agent runtime with ves auth login openai --env OPENAI_API_KEY")
		return
	}
	var body struct {
		Prompt       string `json:"prompt"`
		RepositoryID string `json:"repository_id,omitempty"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	run, err := s.agentApp().StartAgentRun(r.Context(), r.PathValue("id"), body.Prompt, "manual", body.RepositoryID, "")
	if err != nil {
		writeAPIError(w, 409, "agent_run_rejected", err.Error())
		return
	}
	writeJSON(w, 202, run)
}
func (s *Server) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.DB.ListAgentRuns(r.Context(), r.URL.Query().Get("agent_id"))
	if err != nil {
		writeAPIError(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"runs": runs})
}
func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, 404, "not_found", err.Error())
		return
	}
	events, _ := s.DB.ListAgentRunEvents(r.Context(), run.ID, 0)
	attempts, _ := s.DB.ListAgentRunAttempts(r.Context(), run.ID)
	var evaluation any
	evaluations, _ := s.DB.ListAgentEvaluations(r.Context(), run.AgentID)
	for i := range evaluations {
		if evaluations[i].EvaluatedRunID == run.ID {
			evaluation = evaluations[i]
			break
		}
	}
	writeJSON(w, 200, map[string]any{"run": run, "events": events, "attempts": attempts, "evaluation": evaluation})
}
func (s *Server) handleAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	events, err := s.DB.ListAgentRunEvents(r.Context(), r.PathValue("id"), after)
	if err != nil {
		writeAPIError(w, 500, "internal", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}
func (s *Server) handleCancelAgentRun(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.CancelAgentRun(r.Context(), r.PathValue("id")); err != nil {
		writeAPIError(w, 409, "cancel_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"cancel_requested": true})
}
func (s *Server) handleAgentTools(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"schema": "vessica.agent-tools/v1", "tools": generalagent.ToolCatalog()})
}

func (s *Server) handleAgentRunStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, 500, "stream_unavailable", "streaming unavailable")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil && n > after {
			after = n
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	poll := time.NewTicker(350 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			events, err := s.DB.ListAgentRunEvents(r.Context(), r.PathValue("id"), after)
			if err != nil {
				return
			}
			for _, e := range events {
				var payload any
				_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
				record := streaming.ProtocolRecord{Schema: streaming.ProtocolSchema, Kind: "event", RunID: e.RunID, Seq: e.Seq, Timestamp: e.CreatedAt, Event: &streaming.ProtocolEvent{ID: e.ID, RunID: e.RunID, Seq: e.Seq, Type: e.Type, Payload: payload, CreatedAt: e.CreatedAt}}
				raw, _ := json.Marshal(record)
				fmt.Fprintf(w, "id: %d\nevent: event\ndata: %s\n\n", e.Seq, raw)
				after = e.Seq
			}
			if len(events) > 0 {
				flusher.Flush()
			}
			run, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
			if err == nil && (run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled") {
				raw, _ := json.Marshal(streaming.ResultRecord(run.ID, run, map[bool]error{true: fmt.Errorf("%s", run.TerminalError), false: nil}[run.Status == "failed"]))
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", raw)
				flusher.Flush()
				return
			}
		}
	}
}
