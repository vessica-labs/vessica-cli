package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type agentRunInput struct {
	AgentID        string `json:"agent_id" jsonschema:"agent ID or name"`
	Prompt         string `json:"prompt" jsonschema:"prompt for the durable agent run"`
	RepositoryID   string `json:"repository_id,omitempty"`
	ParentRunID    string `json:"parent_run_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable retry key"`
}
type agentRunOutput struct {
	Trigger *state.AgentRunTrigger `json:"trigger,omitempty"`
	Error   *MCPToolError          `json:"error,omitempty"`
}

func (o *agentRunOutput) setMCPError(err *MCPToolError) { o.Error = err }

type conversationSendInput struct {
	ConversationID string `json:"conversation_id,omitempty" jsonschema:"existing conversation ID; omit to create one"`
	Title          string `json:"title,omitempty" jsonschema:"title used when creating a conversation"`
	Message        string `json:"message" jsonschema:"user message"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable retry key"`
}
type conversationSendOutput struct {
	Conversation *state.Conversation        `json:"conversation,omitempty"`
	Message      *state.ConversationMessage `json:"message,omitempty"`
	Error        *MCPToolError              `json:"error,omitempty"`
}

func (o *conversationSendOutput) setMCPError(err *MCPToolError) { o.Error = err }

type subscriptionUpsertInput struct {
	SourceKey      string         `json:"source_key" jsonschema:"stable source key"`
	SourceURL      string         `json:"source_url" jsonschema:"HTTPS source URL"`
	Title          string         `json:"title,omitempty"`
	RetentionDays  int            `json:"retention_days,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"stable retry key"`
}
type subscriptionOutput struct {
	Subscription *state.NewsletterSubscription `json:"subscription,omitempty"`
	Error        *MCPToolError                 `json:"error,omitempty"`
}

func (o *subscriptionOutput) setMCPError(err *MCPToolError) { o.Error = err }

type subscriptionDisableInput struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"subscription ID or source key"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable retry key"`
}

type scheduledWriteProbeInput struct {
	IdempotencyKey string `json:"idempotency_key" jsonschema:"non-sensitive retry key"`
	Note           string `json:"note,omitempty" jsonschema:"non-sensitive diagnostic note; do not include credentials"`
}
type scheduledWriteProbeOutput struct {
	Accepted bool          `json:"accepted"`
	Error    *MCPToolError `json:"error,omitempty"`
}

func (o *scheduledWriteProbeOutput) setMCPError(err *MCPToolError) { o.Error = err }

func (s *Server) registerMCPWriteTools(server *mcp.Server) {
	s.registerOutlookIngestionTool(server)
	addMCPTool(s, server, &mcp.Tool{Name: "agent_run", Description: "Idempotently queue a durable Vessica agent run.", Annotations: additiveWriteAnnotations("Run agent", true)}, mcpToolOptions{Scope: "agents:run", RequiresIdempotency: true}, func() *agentRunOutput { return &agentRunOutput{} }, func(ctx context.Context, _ mcpPrincipal, in agentRunInput) (*agentRunOutput, error) {
		if strings.TrimSpace(in.AgentID) == "" || strings.TrimSpace(in.Prompt) == "" {
			return &agentRunOutput{}, fmt.Errorf("agent_id and prompt are required")
		}
		trigger, err := s.agentApp().TriggerCloudAgentRun(ctx, state.AgentRunTriggerInput{
			AgentID: in.AgentID, IdempotencyKey: in.IdempotencyKey, Trigger: "mcp", InputJSON: mustMarshalJSON(map[string]string{"prompt": in.Prompt}),
			RepositoryID: in.RepositoryID, ParentRunID: in.ParentRunID,
		})
		return &agentRunOutput{Trigger: trigger}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "conversation_send", Description: "Idempotently append one user message to a shared Vessica conversation.", Annotations: additiveWriteAnnotations("Send conversation message", false)}, mcpToolOptions{Scope: "conversations:write", RequiresIdempotency: true}, func() *conversationSendOutput { return &conversationSendOutput{} }, func(ctx context.Context, principal mcpPrincipal, in conversationSendInput) (*conversationSendOutput, error) {
		if strings.TrimSpace(in.Message) == "" {
			return &conversationSendOutput{}, fmt.Errorf("message is required")
		}
		var conversation *state.Conversation
		var err error
		if strings.TrimSpace(in.ConversationID) == "" {
			conversation, err = s.agentApp().StartConversation(ctx, state.ConversationInput{ActorID: principal.ActorID, Title: in.Title})
			if err != nil {
				return &conversationSendOutput{}, err
			}
			in.ConversationID = conversation.ID
		}
		message, err := s.agentApp().AddConversationMessage(ctx, in.ConversationID, state.ConversationMessageInput{Role: "user", ContentJSON: mustMarshalJSON(map[string]string{"text": in.Message}), MetadataJSON: mustMarshalJSON(map[string]string{"source": "mcp"})})
		return &conversationSendOutput{Conversation: conversation, Message: message}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "subscription_upsert", Description: "Idempotently create or update a workspace source subscription.", Annotations: additiveWriteAnnotations("Upsert subscription", true)}, mcpToolOptions{Scope: "sources:manage", RequiresIdempotency: true}, func() *subscriptionOutput { return &subscriptionOutput{} }, func(ctx context.Context, _ mcpPrincipal, in subscriptionUpsertInput) (*subscriptionOutput, error) {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.SourceURL)), "https://") {
			return &subscriptionOutput{}, fmt.Errorf("source_url must use HTTPS")
		}
		metadata, _ := json.Marshal(in.Metadata)
		subscription, err := s.agentApp().UpsertNewsletterSubscription(ctx, state.NewsletterSubscriptionInput{SourceKey: in.SourceKey, SourceURL: in.SourceURL, Title: in.Title, Status: "active", RetentionDays: in.RetentionDays, MetadataJSON: string(metadata)})
		return &subscriptionOutput{Subscription: subscription}, err
	})
	addMCPTool(s, server, &mcp.Tool{Name: "subscription_disable", Description: "Idempotently disable a workspace source subscription without deleting history.", Annotations: additiveWriteAnnotations("Disable subscription", true)}, mcpToolOptions{Scope: "sources:manage", RequiresIdempotency: true}, func() *subscriptionOutput { return &subscriptionOutput{} }, func(ctx context.Context, _ mcpPrincipal, in subscriptionDisableInput) (*subscriptionOutput, error) {
		subscription, err := s.agentApp().DisableNewsletterSubscription(ctx, in.SubscriptionID)
		return &subscriptionOutput{Subscription: subscription}, err
	})
	addMCPTool(s, server, &mcp.Tool{
		Name: "scheduled_write_probe", Description: "Record one non-sensitive idempotent Scheduled Task write-path probe in the Vessica action ledger.",
		Annotations: additiveWriteAnnotations("Scheduled write probe", false),
	}, mcpToolOptions{Scope: "conversations:write", RequiresIdempotency: true}, func() *scheduledWriteProbeOutput { return &scheduledWriteProbeOutput{} }, func(_ context.Context, _ mcpPrincipal, _ scheduledWriteProbeInput) (*scheduledWriteProbeOutput, error) {
		return &scheduledWriteProbeOutput{Accepted: true}, nil
	})
}
