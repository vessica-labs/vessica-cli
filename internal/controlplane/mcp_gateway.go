package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

const mcpMaxBodyBytes = 1 << 20

type mcpPrincipal struct {
	ActorID, WorkspaceID, ClientID string
	Scopes                         map[string]bool
}

type mcpPrincipalKey struct{}
type mcpAuditMarkerKey struct{}
type mcpAuditMarker struct{ recorded bool }

type MCPToolError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type mcpToolOutput interface {
	setMCPError(*MCPToolError)
}

type mcpToolOptions struct {
	Scope               string
	RequiresIdempotency bool
}

func (s *Server) registerMCPRoute(mux *http.ServeMux) {
	sdkHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.newMCPServer(r)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	mux.Handle("POST /mcp", s.requireMCPAccess(sdkHandler))
	mux.HandleFunc("OPTIONS /mcp", s.handleMCPPreflight)
}

func (s *Server) requireMCPAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, resource, ok := s.requireCanonicalMCP(w)
		if !ok {
			return
		}
		if !s.allowMCPOrigin(w, r) {
			return
		}
		rawBody, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBodyBytes+1))
		if err != nil {
			writeMCPHTTPError(w, http.StatusBadRequest, "invalid_request", "MCP request body could not be read")
			return
		}
		if len(rawBody) > mcpMaxBodyBytes {
			writeMCPHTTPError(w, http.StatusRequestEntityTooLarge, "request_too_large", "MCP request body exceeds limit")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(rawBody))
		if !json.Valid(rawBody) {
			if auditErr := s.recordUnauthorizedMCPInvocation(r, rawBody, "invalid_json"); auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "malformed request denial could not be recorded")
				return
			}
			writeMCPHTTPError(w, http.StatusBadRequest, "invalid_request", "MCP request must be valid JSON")
			return
		}
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if provided == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if auditErr := s.recordUnauthorizedMCPInvocation(r, rawBody, "missing_token"); auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "authorization denial could not be recorded")
				return
			}
			s.writeMCPUnauthorized(w, r)
			return
		}
		token, err := s.agentApp().ValidateOAuthAccessToken(r.Context(), hashOAuthMaterial(provided), resource)
		if err != nil {
			if auditErr := s.recordUnauthorizedMCPInvocation(r, rawBody, "invalid_token"); auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "authorization denial could not be recorded")
				return
			}
			s.writeMCPUnauthorized(w, r)
			return
		}
		var scopes []string
		if json.Unmarshal([]byte(token.ScopesJSON), &scopes) != nil {
			if auditErr := s.recordUnauthorizedMCPInvocation(r, rawBody, "invalid_scopes"); auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "scope denial could not be recorded")
				return
			}
			writeMCPHTTPError(w, http.StatusUnauthorized, "invalid_token", "MCP credential scopes are invalid")
			return
		}
		principal := mcpPrincipal{ActorID: token.ActorID, WorkspaceID: token.WorkspaceID, ClientID: token.ClientID, Scopes: map[string]bool{}}
		for _, scope := range scopes {
			principal.Scopes[scope] = true
		}
		tool, arguments := mcpInvocation(rawBody)
		marker := &mcpAuditMarker{}
		if tool != "" && !knownMCPTool(tool) {
			_, auditErr := s.agentApp().RecordAction(r.Context(), state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool, PolicyDecision: "denied", Source: "mcp",
				RedactedArgumentsJSON: redactMCPIdempotencyArgument(arguments), ResultJSON: `{"error":"unknown_tool"}`,
				IdempotencyKey: hashedAuditKey(principal, tool, id.New("audit")), ExternalIDsJSON: "[]",
			})
			if auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "unknown tool denial could not be recorded")
				return
			}
			marker.recorded = true
		}
		ctx := context.WithValue(r.Context(), mcpPrincipalKey{}, principal)
		ctx = context.WithValue(ctx, mcpAuditMarkerKey{}, marker)
		buffered := httptest.NewRecorder()
		next.ServeHTTP(buffered, r.WithContext(ctx))
		if tool != "" && !marker.recorded {
			_, auditErr := s.agentApp().RecordAction(r.Context(), state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool, PolicyDecision: "denied", Source: "mcp",
				RedactedArgumentsJSON: redactMCPIdempotencyArgument(arguments), ResultJSON: `{"error":"pre_handler_denied"}`,
				IdempotencyKey: hashedAuditKey(principal, tool, id.New("audit")), ExternalIDsJSON: "[]",
			})
			if auditErr != nil {
				writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "transport denial could not be recorded")
				return
			}
			buffered.Body = bytes.NewBuffer(stableMCPToolDenial(buffered.Body.Bytes(), "invalid_arguments", "tool arguments did not match the published schema"))
		}
		for key, values := range buffered.Header() {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(buffered.Code)
		_, _ = w.Write(buffered.Body.Bytes())
	})
}

func stableMCPToolDenial(body []byte, code, message string) []byte {
	var response map[string]any
	if json.Unmarshal(body, &response) != nil {
		return body
	}
	errorEnvelope := map[string]any{"code": code, "message": message, "retryable": false}
	response["result"] = map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": message}},
		"structuredContent": map[string]any{"error": errorEnvelope},
		"isError":           true,
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return body
	}
	return encoded
}

func (s *Server) writeMCPUnauthorized(w http.ResponseWriter, r *http.Request) {
	base, _, err := s.canonicalMCPResource()
	if err != nil {
		writeMCPHTTPError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
		return
	}
	metadata := base + "/.well-known/oauth-protected-resource/mcp"
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadata+`", error="invalid_token"`)
	writeMCPHTTPError(w, http.StatusUnauthorized, "invalid_token", "valid MCP authorization is required")
}

func writeMCPHTTPError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": false}})
}

func (s *Server) recordUnauthorizedMCPInvocation(r *http.Request, rawBody []byte, reason string) error {
	tool, arguments := mcpInvocation(rawBody)
	if tool == "" {
		tool = "mcp_transport"
	}
	principal := mcpPrincipal{ActorID: "anonymous", WorkspaceID: "anonymous", ClientID: "anonymous"}
	_, err := s.agentApp().RecordAction(r.Context(), state.ActionLedgerInput{
		ActorID: "anonymous", Tool: tool, PolicyDecision: "denied", Source: "mcp",
		RedactedArgumentsJSON: redactMCPIdempotencyArgument(arguments), ResultJSON: mustMarshalJSON(map[string]string{"error": reason}),
		IdempotencyKey: hashedAuditKey(principal, tool, id.New("audit")), ExternalIDsJSON: "[]",
	})
	return err
}

func knownMCPTool(tool string) bool {
	for _, known := range []string{
		"knowledge_search", "knowledge_get", "briefing_latest", "agents_list", "agent_get", "agent_runs_list", "agent_run_get",
		"conversation_history", "subscriptions_list", "outlook_ingestion_submit", "agent_run", "conversation_send",
		"subscription_upsert", "subscription_disable", "scheduled_write_probe",
	} {
		if tool == known {
			return true
		}
	}
	return false
}

func (s *Server) allowMCPOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if origin == "" {
		return true
	}
	base, _, err := s.canonicalMCPResource()
	if err != nil {
		writeMCPHTTPError(w, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
		return false
	}
	allowed := origin == base
	for _, candidate := range s.MCPAllowedOrigins {
		canonical, err := canonicalHTTPSOrigin(candidate)
		if err == nil && origin == canonical {
			allowed = true
		}
	}
	if !allowed {
		if auditErr := s.recordUnauthorizedMCPInvocation(r, nil, "origin_denied"); auditErr != nil {
			writeMCPHTTPError(w, http.StatusServiceUnavailable, "audit_failed", "origin denial could not be recorded")
			return false
		}
		writeMCPHTTPError(w, http.StatusForbidden, "origin_denied", "request origin is not allowed")
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	return true
}

func (s *Server) handleMCPPreflight(w http.ResponseWriter, r *http.Request) {
	if !s.allowMCPOrigin(w, r) {
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func mcpInvocation(rawBody []byte) (string, json.RawMessage) {
	var request struct {
		Method string `json:"method"`
		Params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if json.Unmarshal(rawBody, &request) != nil || request.Method != "tools/call" {
		return "", nil
	}
	if len(request.Params.Arguments) == 0 {
		request.Params.Arguments = json.RawMessage(`{}`)
	}
	return request.Params.Name, request.Params.Arguments
}

func (s *Server) newMCPServer(_ *http.Request) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "vessica", Version: version.Version}, &mcp.ServerOptions{PageSize: 100})
	s.registerMCPReadTools(server)
	s.registerMCPWriteTools(server)
	return server
}

func addMCPTool[In any, Out mcpToolOutput](s *Server, server *mcp.Server, tool *mcp.Tool, options mcpToolOptions, newOutput func() Out, invoke func(context.Context, mcpPrincipal, In) (Out, error)) {
	if tool.Meta == nil {
		tool.Meta = mcp.Meta{}
	}
	tool.Meta["vessica/scopes"] = []string{options.Scope}
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		if marker, ok := ctx.Value(mcpAuditMarkerKey{}).(*mcpAuditMarker); ok {
			marker.recorded = true
		}
		started := time.Now()
		principal, ok := ctx.Value(mcpPrincipalKey{}).(mcpPrincipal)
		arguments, _ := json.Marshal(in)
		auditArguments := redactMCPIdempotencyArgument(arguments)
		rawKey, durableKey, keyErr := mcpIdempotencyKey(arguments)
		auditKey := hashedAuditKey(principal, tool.Name, durableKey)
		if rawKey == "" {
			auditKey = hashedAuditKey(principal, tool.Name, id.New("audit"))
		}
		deny := func(toolErr *MCPToolError) (*mcp.CallToolResult, Out, error) {
			out := newOutput()
			out.setMCPError(toolErr)
			resultJSON, _ := json.Marshal(out)
			_, auditErr := s.agentApp().RecordAction(ctx, state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool.Name, PolicyDecision: "denied", Source: "mcp",
				RedactedArgumentsJSON: auditArguments, ResultJSON: string(resultJSON),
				LatencyMS: time.Since(started).Milliseconds(), IdempotencyKey: auditKey, ExternalIDsJSON: "[]",
			})
			if auditErr != nil {
				out = newOutput()
				out.setMCPError(&MCPToolError{Code: "audit_failed", Message: "the denial could not be recorded", Retryable: true})
			}
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		if !ok || !principal.Scopes[options.Scope] {
			return deny(&MCPToolError{Code: "insufficient_scope", Message: "required scope is missing", Retryable: false, Details: map[string]any{"required_scope": options.Scope}})
		}
		if keyErr != nil {
			return deny(&MCPToolError{Code: "invalid_idempotency_key", Message: keyErr.Error(), Retryable: false})
		}
		if options.RequiresIdempotency && rawKey == "" {
			return deny(&MCPToolError{Code: "idempotency_required", Message: "idempotency_key is required", Retryable: false})
		}
		var claim *state.ActionExecutionClaim
		if options.RequiresIdempotency {
			var claimErr error
			claim, claimErr = s.agentApp().ClaimAction(ctx, state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool.Name, PolicyDecision: "allowed", Source: "mcp",
				RedactedArgumentsJSON: auditArguments, ArgumentsHash: actionClaimHash(auditArguments), IdempotencyKey: auditKey, ExternalIDsJSON: "[]",
			}, time.Minute)
			if claimErr != nil {
				if errors.Is(claimErr, state.ErrActionIdempotencyConflict) {
					auditKey = hashedAuditKey(principal, tool.Name, auditKey+"\x00"+actionClaimHash(auditArguments))
					return deny(&MCPToolError{Code: "idempotency_conflict", Message: "the idempotency key was already used with different arguments", Retryable: false})
				}
				out := newOutput()
				out.setMCPError(&MCPToolError{Code: "audit_failed", Message: "the invocation could not be claimed", Retryable: true})
				return &mcp.CallToolResult{IsError: true}, out, nil
			}
			if claim.Replay {
				out := newOutput()
				if json.Unmarshal([]byte(claim.Ledger.ResultJSON), out) == nil {
					var envelope struct {
						Error *MCPToolError `json:"error"`
					}
					_ = json.Unmarshal([]byte(claim.Ledger.ResultJSON), &envelope)
					return &mcp.CallToolResult{IsError: envelope.Error != nil}, out, nil
				}
			}
			if !claim.Acquired {
				out := newOutput()
				out.setMCPError(&MCPToolError{Code: "execution_in_progress", Message: "an identical invocation is already executing", Retryable: true})
				return &mcp.CallToolResult{IsError: true}, out, nil
			}
		}
		out, err := invoke(ctx, principal, in)
		if err != nil {
			out = newOutput()
			out.setMCPError(&MCPToolError{Code: "operation_failed", Message: "Vessica could not complete the operation", Retryable: true})
		}
		resultJSON, _ := json.Marshal(out)
		var auditErr error
		if options.RequiresIdempotency {
			if err != nil {
				auditErr = s.agentApp().FailAction(ctx, claim.Ledger.ID, claim.ClaimToken, string(resultJSON))
			} else {
				auditErr = s.agentApp().CompleteAction(ctx, claim.Ledger.ID, claim.ClaimToken, string(resultJSON), time.Since(started).Milliseconds())
			}
		} else {
			_, auditErr = s.agentApp().RecordAction(ctx, state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool.Name, PolicyDecision: "allowed", Source: "mcp",
				RedactedArgumentsJSON: auditArguments, ResultJSON: string(resultJSON),
				LatencyMS: time.Since(started).Milliseconds(), IdempotencyKey: auditKey, ExternalIDsJSON: "[]",
			})
		}
		if auditErr != nil {
			out = newOutput()
			out.setMCPError(&MCPToolError{Code: "audit_failed", Message: "the invocation could not be recorded", Retryable: true})
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		return &mcp.CallToolResult{}, out, nil
	})
}

func redactMCPIdempotencyArgument(arguments []byte) string {
	var object map[string]any
	if json.Unmarshal(arguments, &object) == nil {
		if _, present := object["idempotency_key"]; present {
			object["idempotency_key"] = "[HASHED]"
		}
		if encoded, err := json.Marshal(object); err == nil {
			return string(encoded)
		}
	}
	return string(arguments)
}

func mcpIdempotencyKey(arguments []byte) (string, string, error) {
	var value struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = json.Unmarshal(arguments, &value)
	raw := strings.TrimSpace(value.IdempotencyKey)
	if len(raw) > 200 {
		return raw, "", fmt.Errorf("idempotency_key must not exceed 200 bytes")
	}
	if raw == "" {
		return "", "", nil
	}
	sum := sha256.Sum256([]byte(raw))
	return raw, "ik_" + hex.EncodeToString(sum[:]), nil
}

func hashedAuditKey(principal mcpPrincipal, tool, durableKey string) string {
	sum := sha256.Sum256([]byte(principal.WorkspaceID + "\x00" + principal.ActorID + "\x00" + principal.ClientID + "\x00" + tool + "\x00" + durableKey))
	return "mcp_" + hex.EncodeToString(sum[:])
}

func actionClaimHash(arguments string) string {
	sum := sha256.Sum256([]byte(arguments))
	return hex.EncodeToString(sum[:])
}

func durableMCPIdempotency(raw string) (string, error) {
	encoded, _ := json.Marshal(map[string]string{"idempotency_key": raw})
	_, key, err := mcpIdempotencyKey(encoded)
	return key, err
}

func boolPointer(value bool) *bool { return &value }

func closedReadAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false)}
}

func writeAnnotations(title string, openWorld, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: boolPointer(destructive), OpenWorldHint: boolPointer(openWorld)}
}
