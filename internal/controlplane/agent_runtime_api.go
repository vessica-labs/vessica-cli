package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const agentRuntimeProtocol = "vessica.agent-runtime/v1"

func (s *Server) requireAgentRuntimeAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !constantToken(r.Header.Get("Authorization"), s.AgentRuntimeToken) {
			writeAPIError(w, 401, "unauthorized", "agent runtime authorization is required")
			return
		}
		next(w, r)
	}
}

type runtimeCapabilities struct {
	RuntimeVersion   string   `json:"runtime_version"`
	Protocol         string   `json:"protocol"`
	SDKVersion       string   `json:"sdk_version"`
	Models           []string `json:"models"`
	Tools            []string `json:"tools"`
	Concurrency      int      `json:"concurrency"`
	CredentialsReady bool     `json:"credentials_ready"`
}

func validRuntimeCapabilities(c runtimeCapabilities) bool {
	if c.Protocol != agentRuntimeProtocol || !c.CredentialsReady {
		return false
	}
	availableTools := make(map[string]bool, len(c.Tools))
	for _, toolID := range c.Tools {
		availableTools[toolID] = true
	}
	for _, toolID := range generalagent.ToolCatalog() {
		if !availableTools[toolID] {
			return false
		}
	}
	for _, m := range c.Models {
		if m == generalagent.DefaultModel {
			return true
		}
	}
	return false
}
func (s *Server) handleAgentRuntimeCapabilities(w http.ResponseWriter, r *http.Request) {
	var c runtimeCapabilities
	if err := decodeAgentJSON(w, r, &c); err != nil {
		return
	}
	s.agentRuntimeMu.Lock()
	s.agentRuntimeCaps = c
	s.agentRuntimeSeenAt = time.Now().UTC()
	s.agentRuntimeMu.Unlock()
	status := http.StatusOK
	if !validRuntimeCapabilities(c) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]any{"protocol": agentRuntimeProtocol, "accepted": status == 200, "required_models": []string{generalagent.DefaultModel}, "tools": generalagent.ToolCatalog()})
}

func (s *Server) handleAgentRuntimeClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkerID     string              `json:"worker_id"`
		Capabilities runtimeCapabilities `json:"capabilities"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	s.agentRuntimeMu.Lock()
	s.agentRuntimeCaps = body.Capabilities
	s.agentRuntimeSeenAt = time.Now().UTC()
	s.agentRuntimeMu.Unlock()
	if strings.TrimSpace(body.WorkerID) == "" || !validRuntimeCapabilities(body.Capabilities) {
		writeAPIError(w, 409, "runtime_capability_mismatch", "runtime is not ready or lacks required capabilities")
		return
	}
	task, attempt, err := s.DB.ClaimAgentRuntimeTask(r.Context(), body.WorkerID, 60*time.Second)
	if err != nil {
		writeAPIError(w, 500, "claim_failed", err.Error())
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	payload := map[string]any{"protocol": agentRuntimeProtocol, "task": task, "fence_token": task.FenceToken}
	if attempt != nil {
		payload["attempt"] = attempt
	}
	switch task.Kind {
	case "build":
		op, e := s.DB.GetAgentBuild(r.Context(), task.SubjectID)
		if e != nil {
			writeAPIError(w, 500, "claim_payload_failed", e.Error())
			return
		}
		payload["build"] = op
		var buildPayload struct {
			Timezone string `json:"timezone"`
		}
		_ = json.Unmarshal([]byte(task.PayloadJSON), &buildPayload)
		payload["client_timezone"] = buildPayload.Timezone
		if op.AgentID != "" {
			if current, currentErr := s.DB.GetAgent(r.Context(), op.AgentID); currentErr == nil {
				if version, versionErr := s.DB.GetAgentVersion(r.Context(), current.ID, current.CurrentVersion); versionErr == nil {
					payload["current_definition"] = json.RawMessage(version.DefinitionJSON)
				}
			}
		}
		agents, _ := s.DB.ListActiveAgents(r.Context())
		payload["agent_catalog"] = agents
		payload["model_catalog"] = []string{generalagent.DefaultModel}
		payload["tool_catalog"] = generalagent.ToolCatalog()
	case "run", "eval":
		run, e := s.DB.GetAgentRun(r.Context(), task.SubjectID)
		if e != nil {
			writeAPIError(w, 500, "claim_payload_failed", e.Error())
			return
		}
		version, e := s.DB.GetAgentVersion(r.Context(), run.AgentID, run.DefinitionVersion)
		if e != nil {
			writeAPIError(w, 500, "claim_payload_failed", e.Error())
			return
		}
		agents, _ := s.DB.ListActiveAgents(r.Context())
		repositories, _ := s.DB.ListRepositories(r.Context())
		payload["run"] = run
		payload["definition"] = json.RawMessage(version.DefinitionJSON)
		payload["agent_registry"] = agents
		payload["repositories"] = repositories
	}
	writeJSON(w, 200, payload)
}

func (s *Server) handleAgentRuntimeHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubjectID string `json:"subject_id"`
		Fence     string `json:"fence_token"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	cancelled, err := s.DB.HeartbeatAgentAttempt(r.Context(), body.SubjectID, body.Fence, 60*time.Second)
	if err != nil {
		runtimeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"cancel_requested": cancelled})
}

func (s *Server) handleAgentRuntimeTaskFail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence string `json:"fence_token"`
		Error string `json:"error"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	body.Error = redaction.Redact(body.Error)
	if err := s.DB.FailAgentRuntimeTask(r.Context(), r.PathValue("id"), body.Fence, body.Error); err != nil {
		runtimeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"accepted": true})
}

func (s *Server) handleAgentRuntimeBuildComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence      string          `json:"fence_token"`
		Definition json.RawMessage `json:"definition"`
		Warnings   []string        `json:"warnings"`
		Usage      any             `json:"usage"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	var definition generalagent.Definition
	if err := json.Unmarshal(body.Definition, &definition); err != nil {
		_ = s.DB.FailAgentRuntimeTask(r.Context(), r.PathValue("id"), body.Fence, "builder returned invalid structured output")
		writeAPIError(w, 422, "generated_definition_invalid", "builder returned invalid structured output")
		return
	}
	definition.Defaults("UTC")
	if err := definition.Validate(func(model string) bool { return model == generalagent.DefaultModel }); err != nil {
		_ = s.DB.FailAgentRuntimeTask(r.Context(), r.PathValue("id"), body.Fence, err.Error())
		writeAPIError(w, 422, "generated_definition_invalid", err.Error())
		return
	}
	if err := appservice.ValidateAgentOperations(definition); err != nil {
		_ = s.DB.FailAgentRuntimeTask(r.Context(), r.PathValue("id"), body.Fence, err.Error())
		writeAPIError(w, 422, "generated_definition_invalid", err.Error())
		return
	}
	op, err := s.DB.GetAgentBuild(r.Context(), r.PathValue("id"))
	if err != nil {
		runtimeError(w, err)
		return
	}
	if err = s.agentApp().ValidateAgentCritic(r.Context(), op.AgentID, definition.EvalCriticAgentID); err != nil {
		_ = s.DB.FailAgentRuntimeTask(r.Context(), r.PathValue("id"), body.Fence, err.Error())
		writeAPIError(w, 422, "generated_definition_invalid", err.Error())
		return
	}
	body.Definition, _ = json.Marshal(definition)
	for i := range body.Warnings {
		body.Warnings[i] = redaction.Redact(body.Warnings[i])
	}
	warnings, _ := json.Marshal(body.Warnings)
	usage, _ := json.Marshal(body.Usage)
	draft, err := s.DB.CompleteAgentBuild(r.Context(), r.PathValue("id"), body.Fence, string(body.Definition), string(warnings), string(usage))
	if err != nil {
		runtimeError(w, err)
		return
	}
	result := map[string]any{"draft": draft}
	if !op.Review {
		a, e := s.agentApp().ActivateAgentDraft(r.Context(), draft.ID)
		if e != nil {
			writeAPIError(w, 422, "generated_definition_invalid", e.Error())
			return
		}
		result["agent"] = a
	}
	writeJSON(w, 200, result)
}

func (s *Server) handleAgentRuntimeEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence  string `json:"fence_token"`
		Events []struct {
			Ordinal int64           `json:"ordinal"`
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	events := make([]state.AgentRunEvent, 0, len(body.Events))
	for _, e := range body.Events {
		events = append(events, state.AgentRunEvent{AttemptOrdinal: e.Ordinal, Type: e.Type, PayloadJSON: redaction.Redact(string(e.Payload))})
	}
	if err := s.DB.AppendAgentRunEvents(r.Context(), r.PathValue("id"), body.Fence, events); err != nil {
		runtimeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]int{"accepted": len(events)})
}
func (s *Server) handleAgentRuntimeUsage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence string `json:"fence_token"`
		Usage any    `json:"usage"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	usage, _ := json.Marshal(body.Usage)
	if err := s.DB.CheckpointAgentUsage(r.Context(), r.PathValue("id"), body.Fence, string(usage)); err != nil {
		runtimeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"checkpointed": true})
}
func (s *Server) handleAgentRuntimeComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence          string `json:"fence_token"`
		Output         any    `json:"output"`
		Usage          any    `json:"usage"`
		ActualMicroUSD int64  `json:"actual_microusd"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	output, _ := json.Marshal(body.Output)
	usage, _ := json.Marshal(body.Usage)
	redactedOutput := redaction.Redact(string(output))
	if err := s.DB.CompleteAgentRun(r.Context(), r.PathValue("id"), body.Fence, redactedOutput, string(usage), body.ActualMicroUSD); err != nil {
		runtimeError(w, err)
		return
	}
	run, _ := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
	if run != nil && run.Trigger == "eval" {
		_ = s.DB.CompleteAgentEvaluation(r.Context(), run.ID, redactedOutput)
	} else {
		_, _ = s.DB.QueueAgentEvaluation(r.Context(), r.PathValue("id"))
	}
	writeJSON(w, 200, map[string]bool{"completed": true})
}
func (s *Server) handleAgentRuntimeFail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence          string `json:"fence_token"`
		Error          string `json:"error"`
		Usage          any    `json:"usage"`
		ActualMicroUSD int64  `json:"actual_microusd"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	body.Error = redaction.Redact(body.Error)
	usage, _ := json.Marshal(body.Usage)
	if err := s.DB.FailAgentRun(r.Context(), r.PathValue("id"), body.Fence, body.Error, string(usage), body.ActualMicroUSD); err != nil {
		runtimeError(w, err)
		return
	}
	if run, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id")); err == nil && run.Trigger == "eval" && run.Status == "failed" {
		_ = s.DB.FailAgentEvaluation(r.Context(), run.ID, body.Error)
	}
	writeJSON(w, 200, map[string]bool{"failed": true})
}

func (s *Server) handleAgentRuntimeTool(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence     string          `json:"fence_token"`
		Ordinal   int64           `json:"ordinal"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	toolID := r.PathValue("tool_id")
	sum := sha256.Sum256(body.Arguments)
	hash := hex.EncodeToString(sum[:])
	if result, replayed, err := s.DB.ReplayAgentToolCall(r.Context(), r.PathValue("id"), body.Fence, body.Ordinal, toolID, hash); err != nil {
		runtimeError(w, err)
		return
	} else if replayed {
		writeJSON(w, 200, map[string]any{"replayed": true, "result": json.RawMessage(result)})
		return
	}
	if err := s.authorizeAgentTool(r, r.PathValue("id"), toolID); err != nil {
		writeAPIError(w, 403, "tool_not_enabled", err.Error())
		return
	}
	if err := s.DB.BeginAgentToolCall(r.Context(), r.PathValue("id"), body.Fence, body.Ordinal, toolID, hash); err != nil {
		runtimeError(w, err)
		return
	}
	idempotencyKey := r.PathValue("id") + ":" + strconv.FormatInt(body.Ordinal, 10) + ":" + hash
	result, err := s.executeAgentTool(r, toolID, idempotencyKey, body.Arguments)
	if err != nil {
		safeError := redaction.Redact(err.Error())
		_ = s.DB.FailAgentToolCall(r.Context(), r.PathValue("id"), body.Fence, body.Ordinal, safeError)
		writeAPIError(w, 422, "tool_failed", safeError)
		return
	}
	raw, _ := json.Marshal(result)
	if err = s.DB.CompleteAgentToolCall(r.Context(), r.PathValue("id"), body.Fence, body.Ordinal, toolID, hash, string(raw)); err != nil {
		runtimeError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"replayed": false, "result": result})
}

func (s *Server) authorizeAgentTool(r *http.Request, runID, toolID string) error {
	run, err := s.DB.GetAgentRun(r.Context(), runID)
	if err != nil {
		return err
	}
	v, err := s.DB.GetAgentVersion(r.Context(), run.AgentID, run.DefinitionVersion)
	if err != nil {
		return err
	}
	var d generalagent.Definition
	if err = json.Unmarshal([]byte(v.DefinitionJSON), &d); err != nil {
		return err
	}
	for _, t := range d.Tools {
		if t.ID == toolID {
			return nil
		}
	}
	return errors.New("tool is not enabled for this definition version")
}
func (s *Server) executeAgentTool(r *http.Request, toolID, key string, args json.RawMessage) (any, error) {
	switch toolID {
	case "repository.list":
		return s.DB.ListRepositories(r.Context())
	case "coding_run.start":
		var v struct {
			EpicID      string `json:"epic_id"`
			Concurrency int    `json:"concurrency"`
			Preview     bool   `json:"preview"`
			PRMode      string `json:"pr_mode"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		if v.Concurrency == 0 {
			v.Concurrency = 3
		}
		if v.PRMode == "" {
			v.PRMode = "draft"
		}
		run, err := s.DB.CreateRun(r.Context(), v.EpicID, "", s.Config.Runner.Default, s.Config.Runner.Model, s.Config.Runner.ReasoningEffort, "railway", v.Concurrency, v.Preview, v.PRMode, "", "")
		if err != nil {
			return nil, err
		}
		job, err := s.DB.EnqueueJob(r.Context(), "run_epic", runJobPayload{EpicID: v.EpicID}, run.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"run": run, "job": job}, nil
	default:
		service := s.agentApp()
		repositoryScopeID := ""
		if agentToolNeedsRepositoryScope(toolID) {
			run, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
			if err != nil {
				return nil, err
			}
			scope, err := service.EnsureRepositoryKnowledgeScope(r.Context(), run.OriginatingRepositoryID)
			if err != nil {
				return nil, err
			}
			repositoryScopeID = scope.ID
		}
		return service.ExecuteAgentTool(r.Context(), toolID, "agent-tool:"+key, repositoryScopeID, args)
	}
}

func agentToolNeedsRepositoryScope(toolID string) bool {
	switch toolID {
	case "artifact.create", "artifact.version", "memory.create", "memory.version", "entity.create":
		return true
	default:
		return false
	}
}

func (s *Server) handleAgentRuntimeChild(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fence  string `json:"fence_token"`
		Agent  string `json:"agent"`
		Prompt string `json:"prompt"`
	}
	if err := decodeAgentJSON(w, r, &body); err != nil {
		return
	}
	if err := s.DB.ValidateAgentFence(r.Context(), r.PathValue("id"), body.Fence); err != nil {
		runtimeError(w, err)
		return
	}
	if err := s.authorizeAgentTool(r, r.PathValue("id"), "agent.invoke"); err != nil {
		writeAPIError(w, 403, "tool_not_enabled", err.Error())
		return
	}
	target, err := s.DB.GetAgent(r.Context(), body.Agent)
	if err != nil {
		writeAPIError(w, 404, "agent_not_found", err.Error())
		return
	}
	parent, err := s.DB.GetAgentRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAPIError(w, 404, "run_not_found", err.Error())
		return
	}
	for cursor := parent; cursor != nil; {
		if cursor.AgentID == target.ID {
			writeAPIError(w, 409, "agent_cycle", "cannot invoke an ancestor agent")
			return
		}
		if cursor.ParentRunID == "" {
			break
		}
		cursor, _ = s.DB.GetAgentRun(r.Context(), cursor.ParentRunID)
	}
	child, err := s.agentApp().StartAgentRun(r.Context(), target.ID, body.Prompt, "child", parent.OriginatingRepositoryID, parent.ID)
	if err != nil {
		writeAPIError(w, 409, "child_rejected", err.Error())
		return
	}
	if child.Status != "queued" {
		writeJSON(w, 201, map[string]any{"child": child})
		return
	}
	workerID, err := s.DB.AgentAttemptWorker(r.Context(), parent.ID, body.Fence)
	if err != nil {
		runtimeError(w, err)
		return
	}
	task, attempt, err := s.DB.ClaimAgentRuntimeTaskForRun(r.Context(), child.ID, workerID, 60*time.Second)
	if err != nil {
		runtimeError(w, err)
		return
	}
	version, err := s.DB.GetAgentVersion(r.Context(), child.AgentID, child.DefinitionVersion)
	if err != nil {
		writeAPIError(w, 500, "child_payload_failed", err.Error())
		return
	}
	agents, _ := s.DB.ListAgents(r.Context())
	writeJSON(w, 201, map[string]any{"child": child, "execution": map[string]any{"protocol": agentRuntimeProtocol, "task": task, "attempt": attempt, "fence_token": task.FenceToken, "run": child, "definition": json.RawMessage(version.DefinitionJSON), "agent_registry": agents}})
}

func runtimeError(w http.ResponseWriter, err error) {
	if errors.Is(err, state.ErrAgentFenceLost) {
		writeAPIError(w, 409, "stale_fence", "attempt lease is no longer valid")
		return
	}
	writeAPIError(w, 500, "runtime_protocol_error", err.Error())
}
