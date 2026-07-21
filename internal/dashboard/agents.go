package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Agents(r.Context())
	s.respond(w, r, map[string]any{"agents": v}, err)
}
func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	v, err := s.App.Agent(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, err)
}
func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var d generalagent.Definition
	if !s.decode(w, r, &d, 128<<10) {
		return
	}
	v, err := s.App.CreateStructuredAgent(r.Context(), d, map[string]any{"source": "dashboard", "actor": currentActor(r).UserID})
	s.mutationResult(w, r, "agent.create", "agent", "new", v, err)
}
func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	var d generalagent.Definition
	if !s.decode(w, r, &d, 128<<10) {
		return
	}
	v, err := s.App.UpdateStructuredAgent(r.Context(), r.PathValue("id"), d)
	s.mutationResult(w, r, "agent.update", "agent", r.PathValue("id"), v, err)
}
func (s *Server) handleAgentState(w http.ResponseWriter, r *http.Request) {
	a, err := s.DB.GetAgent(r.Context(), r.PathValue("id"))
	state := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/"+r.PathValue("id")+"/")
	if state == "resume" {
		state = "active"
	} else if state == "pause" {
		state = "paused"
	}
	if err == nil {
		err = s.DB.SetAgentState(r.Context(), a.ID, state)
	}
	if err == nil {
		a, err = s.DB.GetAgent(r.Context(), a.ID)
	}
	s.mutationResult(w, r, "agent."+state, "agent", r.PathValue("id"), a, err)
}
func (s *Server) handleAgentBudget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DailyUSD string `json:"daily_usd"`
		Timezone string `json:"timezone"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	released, err := s.App.SetAgentBudget(r.Context(), r.PathValue("id"), body.DailyUSD, body.Timezone)
	s.mutationResult(w, r, "agent.budget", "agent", r.PathValue("id"), map[string]any{"updated": err == nil, "released_runs": released}, err)
}
func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		a, err := s.DB.GetAgent(r.Context(), r.PathValue("id"))
		if err == nil {
			err = s.DB.DisableAgentSchedule(r.Context(), a.ID)
		}
		s.mutationResult(w, r, "agent.heartbeat.disable", "agent", r.PathValue("id"), map[string]bool{"disabled": err == nil}, err)
		return
	}
	var h generalagent.Heartbeat
	if !s.decode(w, r, &h, 16<<10) {
		return
	}
	h.Enabled = true
	err := s.App.SetAgentHeartbeat(r.Context(), r.PathValue("id"), h)
	s.mutationResult(w, r, "agent.heartbeat.set", "agent", r.PathValue("id"), map[string]bool{"updated": err == nil}, err)
}
func (s *Server) handleStartAgentRun(w http.ResponseWriter, r *http.Request) {
	if !s.agentRuntimeReady(w, r) {
		return
	}
	var body struct {
		Prompt       string `json:"prompt"`
		RepositoryID string `json:"repository_id"`
	}
	if !s.decode(w, r, &body, 64<<10) {
		return
	}
	v, err := s.App.StartAgentRun(r.Context(), r.PathValue("id"), body.Prompt, "manual", body.RepositoryID, "")
	s.mutationResult(w, r, "agent.run", "agent", r.PathValue("id"), v, err)
}

func (s *Server) handleCreateAgentBuild(w http.ResponseWriter, r *http.Request) {
	if !s.agentRuntimeReady(w, r) {
		return
	}
	var body struct {
		Description string `json:"description"`
		AgentID     string `json:"agent_id"`
		Review      bool   `json:"review"`
		Timezone    string `json:"timezone"`
	}
	if !s.decode(w, r, &body, 64<<10) {
		return
	}
	v, err := s.App.CreateAgentBuild(r.Context(), body.Description, body.AgentID, currentActor(r).UserID, body.Review, body.Timezone)
	s.mutationResult(w, r, "agent.build", "agent", body.AgentID, v, err)
}
func (s *Server) agentRuntimeReady(w http.ResponseWriter, r *http.Request) bool {
	if s.RuntimeStatus == nil {
		return true
	}
	status := s.RuntimeStatus()
	ready, _ := status["accepted"].(bool)
	if !ready {
		s.fail(w, r, http.StatusServiceUnavailable, "agent_runtime_credentials_required", "Configure the runtime with ves auth login openai --env OPENAI_API_KEY", nil)
	}
	return ready
}
func (s *Server) handleAgentBuilds(w http.ResponseWriter, r *http.Request) {
	builds, err := s.DB.ListAgentBuilds(r.Context())
	s.respond(w, r, map[string]any{"builds": builds}, err)
}
func (s *Server) handleAgentTools(w http.ResponseWriter, r *http.Request) {
	s.respond(w, r, map[string]any{"tools": generalagent.ToolCatalog()}, nil)
}
func (s *Server) handleAgentBuild(w http.ResponseWriter, r *http.Request) {
	buildID := r.PathValue("id")
	if strings.HasPrefix(buildID, "adraft_") {
		_ = s.DB.QueryRow(r.Context(), `SELECT operation_id FROM agent_drafts WHERE id=?`, buildID).Scan(&buildID)
	}
	op, err := s.DB.GetAgentBuild(r.Context(), buildID)
	result := map[string]any{"operation": op}
	if err == nil {
		var did string
		if s.DB.QueryRow(r.Context(), `SELECT id FROM agent_drafts WHERE operation_id=?`, op.ID).Scan(&did) == nil {
			result["draft"], _ = s.DB.GetAgentDraft(r.Context(), did)
		}
	}
	s.respond(w, r, result, err)
}
func (s *Server) handleActivateAgentBuild(w http.ResponseWriter, r *http.Request) {
	draftID := r.PathValue("id")
	var body struct {
		DraftID string `json:"draft_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DraftID != "" {
		draftID = body.DraftID
	}
	v, err := s.App.ActivateAgentDraft(r.Context(), draftID)
	s.mutationResult(w, r, "agent.draft.activate", "agent_draft", draftID, v, err)
}

func (s *Server) handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	v, err := s.DB.ListAgentRuns(r.Context(), r.URL.Query().Get("agent_id"))
	s.respond(w, r, map[string]any{"runs": v}, err)
}
func (s *Server) handleAgentRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
	events, _ := s.DB.ListAgentRunEvents(r.Context(), r.PathValue("id"), 0)
	attempts, _ := s.DB.ListAgentRunAttempts(r.Context(), r.PathValue("id"))
	var evaluation any
	if run != nil {
		evaluations, _ := s.DB.ListAgentEvaluations(r.Context(), run.AgentID)
		for i := range evaluations {
			if evaluations[i].EvaluatedRunID == run.ID {
				evaluation = evaluations[i]
				break
			}
		}
	}
	s.respond(w, r, map[string]any{"run": run, "events": events, "attempts": attempts, "evaluation": evaluation}, err)
}
func (s *Server) handleAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	v, err := s.DB.ListAgentRunEvents(r.Context(), r.PathValue("id"), after)
	s.respond(w, r, map[string]any{"events": v}, err)
}
func (s *Server) handleCancelAgentRun(w http.ResponseWriter, r *http.Request) {
	err := s.DB.CancelAgentRun(r.Context(), r.PathValue("id"))
	s.mutationResult(w, r, "agent.run.cancel", "agent_run", r.PathValue("id"), map[string]bool{"cancel_requested": err == nil}, err)
}
func (s *Server) handleAgentRunStream(w http.ResponseWriter, r *http.Request) {
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
			for _, event := range events {
				var payload any
				_ = json.Unmarshal([]byte(redaction.Redact(event.PayloadJSON)), &payload)
				record := streaming.ProtocolRecord{Schema: streaming.ProtocolSchema, Kind: "event", RunID: event.RunID, Seq: event.Seq, Timestamp: event.CreatedAt, Event: &streaming.ProtocolEvent{ID: event.ID, RunID: event.RunID, Seq: event.Seq, Type: event.Type, Payload: payload, CreatedAt: event.CreatedAt}}
				raw, _ := json.Marshal(record)
				fmt.Fprintf(w, "id: %d\nevent: event\ndata: %s\n\n", event.Seq, raw)
				after = event.Seq
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
