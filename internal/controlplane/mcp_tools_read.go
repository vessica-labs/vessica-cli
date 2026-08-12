package controlplane

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vessica-labs/vessica-cli/internal/state"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type emptyMCPInput struct{}

type knowledgeSearchInput struct {
	Query      string   `json:"query" jsonschema:"search query"`
	ObjectType string   `json:"object_type,omitempty" jsonschema:"optional entity, artifact, or memory filter"`
	Cursor     string   `json:"cursor,omitempty"`
	Limit      int      `json:"limit,omitempty" jsonschema:"result limit from 1 to 100"`
	Scopes     []string `json:"scopes,omitempty" jsonschema:"optional Vessica knowledge scope IDs"`
}
type knowledgeSearchOutput struct {
	Results knowledge.Page[knowledge.SearchResult] `json:"results"`
	Error   *MCPToolError                          `json:"error,omitempty"`
}

func (o *knowledgeSearchOutput) setMCPError(err *MCPToolError) { o.Error = err }

type knowledgeGetInput struct {
	ObjectType string `json:"object_type" jsonschema:"entity, artifact, or memory"`
	ID         string `json:"id" jsonschema:"Vessica object ID"`
}
type knowledgeGetOutput struct {
	Entity   *knowledge.Entity   `json:"entity,omitempty"`
	Artifact *knowledge.Artifact `json:"artifact,omitempty"`
	Memory   *knowledge.Memory   `json:"memory,omitempty"`
	Error    *MCPToolError       `json:"error,omitempty"`
}

func (o *knowledgeGetOutput) setMCPError(err *MCPToolError) { o.Error = err }

type latestBriefingOutput struct {
	Briefing *knowledge.Artifact `json:"briefing,omitempty"`
	Error    *MCPToolError       `json:"error,omitempty"`
}

func (o *latestBriefingOutput) setMCPError(err *MCPToolError) { o.Error = err }

type agentsListOutput struct {
	Agents []mcpAgentSummary `json:"agents,omitempty"`
	Error  *MCPToolError     `json:"error,omitempty"`
}

func (o *agentsListOutput) setMCPError(err *MCPToolError) { o.Error = err }

type agentGetInput struct {
	AgentID string `json:"agent_id" jsonschema:"agent ID or name"`
}
type agentGetOutput struct {
	Agent *mcpAgentDetail `json:"agent,omitempty"`
	Error *MCPToolError   `json:"error,omitempty"`
}

func (o *agentGetOutput) setMCPError(err *MCPToolError) { o.Error = err }

type agentRunsListInput struct {
	AgentID string `json:"agent_id,omitempty" jsonschema:"optional agent ID"`
}
type agentRunsListOutput struct {
	Runs  []mcpAgentRun `json:"runs,omitempty"`
	Error *MCPToolError `json:"error,omitempty"`
}

func (o *agentRunsListOutput) setMCPError(err *MCPToolError) { o.Error = err }

type agentRunGetInput struct {
	RunID string `json:"run_id" jsonschema:"agent run ID"`
}
type agentRunGetOutput struct {
	Run   *mcpAgentRun  `json:"run,omitempty"`
	Error *MCPToolError `json:"error,omitempty"`
}

func (o *agentRunGetOutput) setMCPError(err *MCPToolError) { o.Error = err }

type conversationHistoryInput struct {
	ConversationID string `json:"conversation_id" jsonschema:"conversation ID"`
	AfterSequence  int64  `json:"after_sequence,omitempty" jsonschema:"return messages after this sequence"`
}
type conversationHistoryOutput struct {
	Messages []state.ConversationMessage `json:"messages,omitempty"`
	Error    *MCPToolError               `json:"error,omitempty"`
}

func (o *conversationHistoryOutput) setMCPError(err *MCPToolError) { o.Error = err }

type subscriptionsListOutput struct {
	Subscriptions []state.NewsletterSubscription `json:"subscriptions,omitempty"`
	Error         *MCPToolError                  `json:"error,omitempty"`
}

func (o *subscriptionsListOutput) setMCPError(err *MCPToolError) { o.Error = err }

func (s *Server) registerMCPReadTools(server *mcp.Server) {
	addMCPTool(s, server, &mcp.Tool{Name: "knowledge_search", Description: "Search workspace-scoped Vessica knowledge.", Annotations: closedReadAnnotations("Search knowledge")}, mcpToolOptions{Scope: "knowledge:read"}, func() *knowledgeSearchOutput { return &knowledgeSearchOutput{} }, func(ctx context.Context, _ mcpPrincipal, in knowledgeSearchInput) (*knowledgeSearchOutput, error) {
		if in.Limit <= 0 {
			in.Limit = 20
		}
		if in.Limit > 100 {
			in.Limit = 100
		}
		result, err := s.agentApp().KnowledgeSearch(ctx, in.Query, in.ObjectType, in.Cursor, in.Limit, in.Scopes)
		return &knowledgeSearchOutput{Results: result}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "knowledge_get", Description: "Get one workspace-scoped Vessica entity, artifact, or memory.", Annotations: closedReadAnnotations("Get knowledge")}, mcpToolOptions{Scope: "knowledge:read"}, func() *knowledgeGetOutput { return &knowledgeGetOutput{} }, func(ctx context.Context, _ mcpPrincipal, in knowledgeGetInput) (*knowledgeGetOutput, error) {
		out := &knowledgeGetOutput{}
		switch in.ObjectType {
		case "entity":
			value, err := s.agentApp().Entity(ctx, in.ID)
			out.Entity = &value
			return out, err
		case "artifact":
			value, err := s.agentApp().Artifact(ctx, in.ID)
			out.Artifact = &value
			return out, err
		case "memory":
			value, err := s.agentApp().Memory(ctx, in.ID)
			out.Memory = &value
			return out, err
		default:
			return out, fmt.Errorf("object_type must be entity, artifact, or memory")
		}
	})
	addMCPTool(s, server, &mcp.Tool{Name: "briefing_latest", Description: "Return the newest active briefing artifact in Vessica knowledge.", Annotations: closedReadAnnotations("Latest briefing")}, mcpToolOptions{Scope: "knowledge:read"}, func() *latestBriefingOutput { return &latestBriefingOutput{} }, func(ctx context.Context, _ mcpPrincipal, _ emptyMCPInput) (*latestBriefingOutput, error) {
		items, err := s.agentApp().Artifacts(ctx, "briefing", "active", nil)
		if err != nil {
			return &latestBriefingOutput{}, err
		}
		if len(items) == 0 {
			return &latestBriefingOutput{}, fmt.Errorf("briefing not found")
		}
		latest := items[0]
		for i := 1; i < len(items); i++ {
			if items[i].UpdatedAt.After(latest.UpdatedAt) {
				latest = items[i]
			}
		}
		return &latestBriefingOutput{Briefing: &latest}, nil
	})
	addMCPTool(s, server, &mcp.Tool{Name: "agents_list", Description: "List durable Vessica agents in the authorized workspace.", Annotations: closedReadAnnotations("List agents")}, mcpToolOptions{Scope: "agents:read"}, func() *agentsListOutput { return &agentsListOutput{} }, func(ctx context.Context, principal mcpPrincipal, _ emptyMCPInput) (*agentsListOutput, error) {
		agents, err := s.agentApp().AgentsForWorkspace(ctx, principal.WorkspaceID)
		return &agentsListOutput{Agents: publicAgentSummaries(agents)}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "agent_get", Description: "Get one durable Vessica agent and its active definition.", Annotations: closedReadAnnotations("Get agent")}, mcpToolOptions{Scope: "agents:read"}, func() *agentGetOutput { return &agentGetOutput{} }, func(ctx context.Context, principal mcpPrincipal, in agentGetInput) (*agentGetOutput, error) {
		agent, err := s.agentApp().AgentForWorkspace(ctx, principal.WorkspaceID, in.AgentID)
		return &agentGetOutput{Agent: publicAgentDetail(agent)}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "agent_runs_list", Description: "List durable Vessica agent runs, optionally for one agent.", Annotations: closedReadAnnotations("List agent runs")}, mcpToolOptions{Scope: "agents:read"}, func() *agentRunsListOutput { return &agentRunsListOutput{} }, func(ctx context.Context, principal mcpPrincipal, in agentRunsListInput) (*agentRunsListOutput, error) {
		runs, err := s.agentApp().AgentRunsForWorkspace(ctx, principal.WorkspaceID, in.AgentID)
		return &agentRunsListOutput{Runs: publicAgentRuns(runs)}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "agent_run_get", Description: "Get one durable Vessica agent run.", Annotations: closedReadAnnotations("Get agent run")}, mcpToolOptions{Scope: "agents:read"}, func() *agentRunGetOutput { return &agentRunGetOutput{} }, func(ctx context.Context, principal mcpPrincipal, in agentRunGetInput) (*agentRunGetOutput, error) {
		run, err := s.agentApp().AgentRunForWorkspace(ctx, principal.WorkspaceID, in.RunID)
		if run == nil {
			return &agentRunGetOutput{}, err
		}
		public := publicAgentRun(*run)
		return &agentRunGetOutput{Run: &public}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "conversation_history", Description: "Read ordered messages from a shared workspace conversation.", Annotations: closedReadAnnotations("Conversation history")}, mcpToolOptions{Scope: "conversations:write"}, func() *conversationHistoryOutput { return &conversationHistoryOutput{} }, func(ctx context.Context, _ mcpPrincipal, in conversationHistoryInput) (*conversationHistoryOutput, error) {
		messages, err := s.agentApp().ConversationMessages(ctx, in.ConversationID, in.AfterSequence)
		return &conversationHistoryOutput{Messages: messages}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "subscriptions_list", Description: "List workspace newsletter and source subscriptions.", Annotations: closedReadAnnotations("List subscriptions")}, mcpToolOptions{Scope: "sources:manage"}, func() *subscriptionsListOutput { return &subscriptionsListOutput{} }, func(ctx context.Context, _ mcpPrincipal, _ emptyMCPInput) (*subscriptionsListOutput, error) {
		subscriptions, err := s.agentApp().NewsletterSubscriptions(ctx)
		return &subscriptionsListOutput{Subscriptions: subscriptions}, err
	})
}
