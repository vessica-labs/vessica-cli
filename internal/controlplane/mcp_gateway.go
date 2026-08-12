package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
}

func (s *Server) requireMCPAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if provided == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			s.recordUnauthorizedMCPInvocation(r, rawBody, "missing_token")
			s.writeMCPUnauthorized(w, r)
			return
		}
		token, err := s.agentApp().ValidateOAuthAccessToken(r.Context(), hashOAuthMaterial(provided))
		if err != nil {
			s.recordUnauthorizedMCPInvocation(r, rawBody, "invalid_token")
			s.writeMCPUnauthorized(w, r)
			return
		}
		var scopes []string
		if json.Unmarshal([]byte(token.ScopesJSON), &scopes) != nil {
			writeMCPHTTPError(w, http.StatusUnauthorized, "invalid_token", "MCP credential scopes are invalid")
			return
		}
		principal := mcpPrincipal{ActorID: token.ActorID, WorkspaceID: token.WorkspaceID, ClientID: token.ClientID, Scopes: map[string]bool{}}
		for _, scope := range scopes {
			principal.Scopes[scope] = true
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), mcpPrincipalKey{}, principal)))
	})
}

func (s *Server) writeMCPUnauthorized(w http.ResponseWriter, r *http.Request) {
	metadata := s.oauthBaseURL(r) + "/.well-known/oauth-protected-resource/mcp"
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadata+`", error="invalid_token"`)
	writeMCPHTTPError(w, http.StatusUnauthorized, "invalid_token", "valid MCP authorization is required")
}

func writeMCPHTTPError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "retryable": false}})
}

func (s *Server) recordUnauthorizedMCPInvocation(r *http.Request, rawBody []byte, reason string) {
	tool, arguments := mcpInvocation(rawBody)
	if tool == "" {
		return
	}
	_, _ = s.agentApp().RecordAction(r.Context(), state.ActionLedgerInput{
		ActorID: "anonymous", Tool: tool, PolicyDecision: "denied",
		RedactedArgumentsJSON: string(arguments), ResultJSON: mustMarshalJSON(map[string]string{"error": reason}),
		IdempotencyKey: "mcp:anonymous:" + id.New("audit"), ExternalIDsJSON: "[]",
	})
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
		started := time.Now()
		principal, ok := ctx.Value(mcpPrincipalKey{}).(mcpPrincipal)
		arguments, _ := json.Marshal(in)
		idempotencyKey := mcpIdempotencyKey(arguments)
		auditKey := "mcp:" + principal.ActorID + ":" + principal.ClientID + ":" + tool.Name + ":" + idempotencyKey
		if idempotencyKey == "" {
			auditKey = "mcp:" + principal.ActorID + ":" + principal.ClientID + ":" + tool.Name + ":" + id.New("audit")
		}
		deny := func(toolErr *MCPToolError) (*mcp.CallToolResult, Out, error) {
			out := newOutput()
			out.setMCPError(toolErr)
			resultJSON, _ := json.Marshal(out)
			_, _ = s.agentApp().RecordAction(ctx, state.ActionLedgerInput{
				ActorID: principal.ActorID, Tool: tool.Name, PolicyDecision: "denied",
				RedactedArgumentsJSON: string(arguments), ResultJSON: string(resultJSON),
				LatencyMS: time.Since(started).Milliseconds(), IdempotencyKey: auditKey, ExternalIDsJSON: "[]",
			})
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		if !ok || !principal.Scopes[options.Scope] {
			return deny(&MCPToolError{Code: "insufficient_scope", Message: "required scope is missing", Retryable: false, Details: map[string]any{"required_scope": options.Scope}})
		}
		if options.RequiresIdempotency && idempotencyKey == "" {
			return deny(&MCPToolError{Code: "idempotency_required", Message: "idempotency_key is required", Retryable: false})
		}
		if idempotencyKey != "" {
			if existing, replayErr := s.DB.GetActionLedgerByKey(ctx, auditKey); replayErr == nil {
				out := newOutput()
				if json.Unmarshal([]byte(existing.ResultJSON), out) == nil {
					var envelope struct {
						Error *MCPToolError `json:"error"`
					}
					_ = json.Unmarshal([]byte(existing.ResultJSON), &envelope)
					return &mcp.CallToolResult{IsError: envelope.Error != nil}, out, nil
				}
			}
		}
		out, err := invoke(ctx, principal, in)
		if err != nil {
			out = newOutput()
			out.setMCPError(&MCPToolError{Code: "operation_failed", Message: "Vessica could not complete the operation", Retryable: false})
		}
		resultJSON, _ := json.Marshal(out)
		ledger, auditErr := s.agentApp().RecordAction(ctx, state.ActionLedgerInput{
			ActorID: principal.ActorID, Tool: tool.Name, PolicyDecision: "allowed",
			RedactedArgumentsJSON: string(arguments), ResultJSON: string(resultJSON),
			LatencyMS: time.Since(started).Milliseconds(), IdempotencyKey: auditKey, ExternalIDsJSON: "[]",
		})
		if auditErr != nil {
			out = newOutput()
			out.setMCPError(&MCPToolError{Code: "audit_failed", Message: "the invocation could not be recorded", Retryable: true})
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		_ = ledger
		if err != nil {
			return &mcp.CallToolResult{IsError: true}, out, nil
		}
		return &mcp.CallToolResult{}, out, nil
	})
}

func mcpIdempotencyKey(arguments []byte) string {
	var value struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = json.Unmarshal(arguments, &value)
	return strings.TrimSpace(value.IdempotencyKey)
}

func boolPointer(value bool) *bool { return &value }

func closedReadAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(false)}
}

func additiveWriteAnnotations(title string, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(openWorld)}
}
