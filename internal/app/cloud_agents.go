package app

import (
	"context"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

// The control-plane application surface deliberately delegates durable rules
// to state. HTTP and MCP adapters can share these methods without importing
// one another or handling direct SQL.
func (s *Service) RegisterOAuthClient(ctx context.Context, in state.OAuthClientInput) (*state.OAuthClient, error) {
	return s.DB.UpsertOAuthClient(ctx, in)
}
func (s *Service) ConsumeOAuthAuthorizationCode(ctx context.Context, codeHash string) (*state.OAuthAuthorizationCode, error) {
	return s.DB.ConsumeOAuthAuthorizationCode(ctx, codeHash)
}
func (s *Service) ValidateOAuthAccessToken(ctx context.Context, tokenHash string) (*state.OAuthAccessToken, error) {
	return s.DB.GetOAuthAccessToken(ctx, tokenHash)
}
func (s *Service) RecordAction(ctx context.Context, in state.ActionLedgerInput) (*state.ActionLedger, error) {
	return s.DB.AppendActionLedger(ctx, in)
}
func (s *Service) StartConversation(ctx context.Context, in state.ConversationInput) (*state.Conversation, error) {
	return s.DB.CreateConversation(ctx, in)
}
func (s *Service) AddConversationMessage(ctx context.Context, conversationID string, in state.ConversationMessageInput) (*state.ConversationMessage, error) {
	return s.DB.AppendConversationMessage(ctx, conversationID, in)
}
func (s *Service) ConversationMessages(ctx context.Context, conversationID string, after int64) ([]state.ConversationMessage, error) {
	return s.DB.ListConversationMessages(ctx, conversationID, after)
}
func (s *Service) UpsertNewsletterSubscription(ctx context.Context, in state.NewsletterSubscriptionInput) (*state.NewsletterSubscription, error) {
	return s.DB.UpsertNewsletterSubscription(ctx, in)
}
func (s *Service) SubmitOutlookIngestion(ctx context.Context, in state.OutlookIngestionBatchInput) (*state.OutlookIngestionBatch, error) {
	return s.DB.CreateOutlookIngestionBatch(ctx, in)
}
func (s *Service) TriggerCloudAgentRun(ctx context.Context, in state.AgentRunTriggerInput) (*state.AgentRunTrigger, error) {
	return s.DB.TriggerAgentRun(ctx, in)
}
