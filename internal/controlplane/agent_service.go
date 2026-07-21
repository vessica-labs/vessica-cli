package controlplane

import (
	"context"
	"net/http"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
)

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/agent-builds", s.requireAPIAuth(s.requireIdempotency(s.handleCreateAgentBuild)))
	mux.HandleFunc("GET /api/v1/agent-builds", s.requireAPIAuth(s.handleAgentBuilds))
	mux.HandleFunc("GET /api/v1/agent-builds/{id}", s.requireAPIAuth(s.handleAgentBuild))
	mux.HandleFunc("POST /api/v1/agent-builds/{id}/activate", s.requireAPIAuth(s.requireIdempotency(s.handleActivateAgentBuild)))
	mux.HandleFunc("GET /api/v1/agents", s.requireAPIAuth(s.handleAgents))
	mux.HandleFunc("POST /api/v1/agents", s.requireAPIAuth(s.requireIdempotency(s.handleCreateAgent)))
	mux.HandleFunc("GET /api/v1/agents/{id}", s.requireAPIAuth(s.handleAgent))
	mux.HandleFunc("PATCH /api/v1/agents/{id}", s.requireAPIAuth(s.requireIdempotency(s.handleUpdateAgent)))
	mux.HandleFunc("POST /api/v1/agents/{id}/pause", s.requireAPIAuth(s.requireIdempotency(s.handleAgentState)))
	mux.HandleFunc("POST /api/v1/agents/{id}/resume", s.requireAPIAuth(s.requireIdempotency(s.handleAgentState)))
	mux.HandleFunc("POST /api/v1/agents/{id}/archive", s.requireAPIAuth(s.requireIdempotency(s.handleAgentState)))
	mux.HandleFunc("PUT /api/v1/agents/{id}/budget", s.requireAPIAuth(s.requireIdempotency(s.handleAgentBudget)))
	mux.HandleFunc("PUT /api/v1/agents/{id}/heartbeat", s.requireAPIAuth(s.requireIdempotency(s.handleAgentHeartbeat)))
	mux.HandleFunc("DELETE /api/v1/agents/{id}/heartbeat", s.requireAPIAuth(s.requireIdempotency(s.handleAgentHeartbeat)))
	mux.HandleFunc("POST /api/v1/agents/{id}/runs", s.requireAPIAuth(s.requireIdempotency(s.handleStartAgentRun)))
	mux.HandleFunc("GET /api/v1/agent-runs", s.requireAPIAuth(s.handleAgentRuns))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}", s.requireAPIAuth(s.handleAgentRun))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}/events", s.requireAPIAuth(s.handleAgentRunEvents))
	mux.HandleFunc("GET /api/v1/agent-runs/{id}/stream", s.requireAPIAuth(s.handleAgentRunStream))
	mux.HandleFunc("POST /api/v1/agent-runs/{id}/cancel", s.requireAPIAuth(s.requireIdempotency(s.handleCancelAgentRun)))
	mux.HandleFunc("GET /api/v1/agent-tools", s.requireAPIAuth(s.handleAgentTools))
	mux.HandleFunc("POST /internal/agent-runtime/v1/capabilities", s.requireAgentRuntimeAuth(s.handleAgentRuntimeCapabilities))
	mux.HandleFunc("POST /internal/agent-runtime/v1/tasks/claim", s.requireAgentRuntimeAuth(s.handleAgentRuntimeClaim))
	mux.HandleFunc("POST /internal/agent-runtime/v1/tasks/{id}/heartbeat", s.requireAgentRuntimeAuth(s.handleAgentRuntimeHeartbeat))
	mux.HandleFunc("POST /internal/agent-runtime/v1/tasks/{id}/fail", s.requireAgentRuntimeAuth(s.handleAgentRuntimeTaskFail))
	mux.HandleFunc("POST /internal/agent-runtime/v1/builds/{id}/complete", s.requireAgentRuntimeAuth(s.handleAgentRuntimeBuildComplete))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/events", s.requireAgentRuntimeAuth(s.handleAgentRuntimeEvents))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/usage", s.requireAgentRuntimeAuth(s.handleAgentRuntimeUsage))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/complete", s.requireAgentRuntimeAuth(s.handleAgentRuntimeComplete))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/fail", s.requireAgentRuntimeAuth(s.handleAgentRuntimeFail))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/tools/{tool_id}", s.requireAgentRuntimeAuth(s.handleAgentRuntimeTool))
	mux.HandleFunc("POST /internal/agent-runtime/v1/runs/{id}/children", s.requireAgentRuntimeAuth(s.handleAgentRuntimeChild))
}

func (s *Server) agentApp() *appservice.Service { return appservice.New(s.DB, "", s.Config) }

func (s *Server) agentScheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if s.agentRuntimeReady() {
				if err := s.agentApp().TickAgentSchedules(ctx, now); err != nil {
					s.Logger.Printf("agent scheduler: %v", err)
				}
			}
		}
	}
}

func (s *Server) AgentRuntimeStatus() map[string]any {
	s.agentRuntimeMu.RLock()
	defer s.agentRuntimeMu.RUnlock()
	connected := !s.agentRuntimeSeenAt.IsZero() && time.Since(s.agentRuntimeSeenAt) < 2*time.Minute
	return map[string]any{"connected": connected, "credentials_ready": s.agentRuntimeCaps.CredentialsReady, "accepted": connected && validRuntimeCapabilities(s.agentRuntimeCaps), "last_seen_at": s.agentRuntimeSeenAt, "capabilities": s.agentRuntimeCaps}
}

func (s *Server) agentRuntimeReady() bool {
	s.agentRuntimeMu.RLock()
	defer s.agentRuntimeMu.RUnlock()
	return time.Since(s.agentRuntimeSeenAt) < 2*time.Minute && validRuntimeCapabilities(s.agentRuntimeCaps)
}
