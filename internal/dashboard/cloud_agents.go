package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type conversationView struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	AgentID     string `json:"agent_id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type conversationMessageView struct {
	ID           string          `json:"id"`
	Sequence     int64           `json:"sequence"`
	Role         string          `json:"role"`
	Content      map[string]any  `json:"content"`
	Metadata     map[string]any  `json:"metadata"`
	CreatedAt    string          `json:"created_at"`
	Run          *state.AgentRun `json:"run,omitempty"`
	Citations    []string        `json:"citations"`
	ActionLedger string          `json:"action_ledger_url,omitempty"`
}

func publicConversation(value *state.Conversation) conversationView {
	if value == nil {
		return conversationView{}
	}
	return conversationView{ID: value.ID, WorkspaceID: value.WorkspaceID, ActorID: value.ActorID, AgentID: value.AgentID, Title: value.Title, Status: value.Status, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	values, err := s.App.Conversations(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]conversationView, 0, len(values))
	for i := range values {
		out = append(out, publicConversation(&values[i]))
	}
	s.ok(w, map[string]any{"conversations": out}, nil)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title   string `json:"title"`
		AgentID string `json:"agent_id"`
	}
	if !s.decode(w, r, &body, 16<<10) {
		return
	}
	body.Title, body.AgentID = strings.TrimSpace(body.Title), strings.TrimSpace(body.AgentID)
	if body.Title == "" || len(body.Title) > 200 || body.AgentID == "" {
		s.fail(w, r, http.StatusBadRequest, "invalid_conversation", "title and agent_id are required", nil)
		return
	}
	value, err := s.App.StartConversation(r.Context(), state.ConversationInput{ActorID: currentActor(r).UserID, AgentID: body.AgentID, Title: body.Title})
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "invalid_agent", err.Error(), nil)
		return
	}
	s.mutationResult(w, r, "conversation.create", "conversation", value.ID, publicConversation(value), nil)
}

func (s *Server) handleConversation(w http.ResponseWriter, r *http.Request) {
	conversation, err := s.App.Conversation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "not_found", "conversation not found", nil)
		return
	}
	messages, err := s.App.ConversationMessages(r.Context(), conversation.ID, 0)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	workspace, err := s.DB.GetWorkspace(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	views := make([]conversationMessageView, 0, len(messages))
	runs := make([]state.AgentRun, 0, len(messages))
	for i := range messages {
		view := conversationMessageView{ID: messages[i].ID, Sequence: messages[i].Sequence, Role: messages[i].Role, Content: map[string]any{}, Metadata: map[string]any{}, CreatedAt: messages[i].CreatedAt, Citations: []string{}}
		_ = json.Unmarshal([]byte(messages[i].ContentJSON), &view.Content)
		_ = json.Unmarshal([]byte(messages[i].MetadataJSON), &view.Metadata)
		if ledgerID, _ := view.Metadata["action_ledger_id"].(string); ledgerID != "" {
			view.ActionLedger = "/operator#action-" + ledgerID
		}
		if runID, _ := view.Metadata["run_id"].(string); runID != "" {
			if run, runErr := s.App.AgentRunForWorkspace(r.Context(), workspace.ID, runID); runErr == nil && run != nil {
				view.Run = run
				view.Citations = runCitations(run.OutputJSON)
				runs = append(runs, *run)
			}
		}
		views = append(views, view)
	}
	s.ok(w, map[string]any{"conversation": publicConversation(conversation), "messages": views, "runs": runs}, nil)
}

func runCitations(output string) []string {
	var value map[string]any
	if json.Unmarshal([]byte(output), &value) != nil {
		return []string{}
	}
	raw, _ := value["citations"].([]any)
	out := make([]string, 0, len(raw))
	for _, candidate := range raw {
		if citation, ok := candidate.(string); ok && strings.TrimSpace(citation) != "" {
			out = append(out, citation)
		}
	}
	return out
}

func (s *Server) handleConversationMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
		AgentID string `json:"agent_id,omitempty"`
	}
	if !s.decode(w, r, &body, 32<<10) {
		return
	}
	body.Message, body.AgentID = strings.TrimSpace(body.Message), strings.TrimSpace(body.AgentID)
	if body.Message == "" || len(body.Message) > 16000 {
		s.fail(w, r, http.StatusBadRequest, "invalid_message", "message must contain 1-16000 characters", nil)
		return
	}
	conversation, err := s.App.Conversation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "not_found", "conversation not found", nil)
		return
	}
	if body.AgentID != "" && body.AgentID != conversation.AgentID {
		conversation, err = s.App.SelectConversationAgent(r.Context(), conversation.ID, body.AgentID)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "invalid_agent", err.Error(), nil)
			return
		}
	}
	if conversation.AgentID == "" {
		s.fail(w, r, http.StatusBadRequest, "agent_required", "select an agent before sending a message", nil)
		return
	}
	key := "web-conversation:" + conversation.ID + ":" + r.Header.Get("Idempotency-Key")
	input, _ := json.Marshal(map[string]string{"prompt": body.Message, "conversation_id": conversation.ID})
	trigger, err := s.App.TriggerCloudAgentRun(r.Context(), state.AgentRunTriggerInput{AgentID: conversation.AgentID, IdempotencyKey: key, Trigger: "web_conversation", InputJSON: string(input), RateSnapshot: state.DefaultAgentRateSnapshot()})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	started := time.Now()
	ledger, err := s.App.RecordAction(r.Context(), state.ActionLedgerInput{ActorID: currentActor(r).UserID, AgentID: conversation.AgentID, AgentRunID: trigger.AgentRunID, Tool: "conversation_send", PolicyDecision: "allowed", RedactedArgumentsJSON: string(input), ResultJSON: fmt.Sprintf(`{"run_id":%q}`, trigger.AgentRunID), LatencyMS: time.Since(started).Milliseconds(), IdempotencyKey: key, ExternalIDsJSON: fmt.Sprintf(`[%q]`, trigger.AgentRunID)})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	content, _ := json.Marshal(map[string]string{"text": body.Message})
	metadata, _ := json.Marshal(map[string]string{"source": "dashboard", "agent_id": conversation.AgentID, "run_id": trigger.AgentRunID, "action_ledger_id": ledger.ID})
	_, message, _, err := s.App.SendConversationMessageIdempotent(r.Context(), key, currentActor(r).UserID, conversation.ID, conversation.Title, state.ConversationMessageInput{Role: "user", ContentJSON: string(content), MetadataJSON: string(metadata)})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	run, err := s.App.AgentRun(r.Context(), trigger.AgentRunID)
	view := conversationMessageView{ID: message.ID, Sequence: message.Sequence, Role: message.Role, Content: map[string]any{"text": body.Message}, Metadata: map[string]any{"source": "dashboard", "agent_id": conversation.AgentID, "run_id": trigger.AgentRunID, "action_ledger_id": ledger.ID}, CreatedAt: message.CreatedAt, Run: run, Citations: []string{}, ActionLedger: "/operator#action-" + ledger.ID}
	s.mutationResult(w, r, "conversation.message", "conversation", conversation.ID, map[string]any{"conversation": publicConversation(conversation), "message": view, "run": run}, err)
}

func (s *Server) handleBriefings(w http.ResponseWriter, r *http.Request) {
	briefings, err := s.App.Artifacts(r.Context(), "briefing", "active", nil)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	newsletters, err := s.App.Artifacts(r.Context(), "newsletter", "active", nil)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.ok(w, map[string]any{"briefings": newestArtifacts(briefings), "newsletters": newestArtifacts(newsletters)}, nil)
}

func newestArtifacts(values []knowledge.Artifact) []knowledge.Artifact {
	if len(values) < 2 {
		return values
	}
	for left := 0; left < len(values)-1; left++ {
		for right := left + 1; right < len(values); right++ {
			if values[right].UpdatedAt.After(values[left].UpdatedAt) {
				values[left], values[right] = values[right], values[left]
			}
		}
	}
	return values
}

func (s *Server) handleOperator(w http.ResponseWriter, r *http.Request) {
	value, err := s.App.OperatorSnapshot(r.Context(), time.Now())
	s.respond(w, r, value, err)
}
