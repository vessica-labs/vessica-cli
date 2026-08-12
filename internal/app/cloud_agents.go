package app

import (
	"context"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

// The control-plane application surface deliberately delegates durable rules
// to state. HTTP and MCP adapters can share these methods without importing
// one another or handling direct SQL.
func (s *Service) RegisterOAuthClient(ctx context.Context, in state.OAuthClientInput) (*state.OAuthClient, error) {
	return s.DB.UpsertOAuthClient(ctx, in)
}
func (s *Service) CreateOAuthAuthorizationCode(ctx context.Context, in state.OAuthAuthorizationCodeInput) (*state.OAuthAuthorizationCode, error) {
	return s.DB.CreateOAuthAuthorizationCode(ctx, in)
}
func (s *Service) IssueOAuthAccessToken(ctx context.Context, in state.OAuthAccessTokenInput) (*state.OAuthAccessToken, error) {
	return s.DB.IssueOAuthAccessToken(ctx, in)
}
func (s *Service) IssueOAuthRefreshToken(ctx context.Context, in state.OAuthRefreshTokenInput) (*state.OAuthRefreshToken, error) {
	return s.DB.IssueOAuthRefreshToken(ctx, in)
}
func (s *Service) RevokeOAuthAccessToken(ctx context.Context, tokenHash string) error {
	return s.DB.RevokeOAuthAccessToken(ctx, tokenHash)
}
func (s *Service) RevokeOAuthRefreshToken(ctx context.Context, tokenHash string) error {
	return s.DB.RevokeOAuthRefreshToken(ctx, tokenHash)
}
func (s *Service) ValidateOAuthRefreshToken(ctx context.Context, materialHash string) (*state.OAuthRefreshToken, error) {
	return s.DB.GetOAuthRefreshToken(ctx, materialHash)
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
func (s *Service) UpsertNewsletterItem(ctx context.Context, in state.NewsletterItemInput) (*state.NewsletterItem, error) {
	return s.DB.UpsertNewsletterItem(ctx, in)
}
func (s *Service) UpsertSourceCheckpoint(ctx context.Context, typ, sourceID, checkpoint string) error {
	return s.DB.UpsertSourceCheckpoint(ctx, typ, sourceID, checkpoint)
}
func (s *Service) SourceCheckpoint(ctx context.Context, typ, sourceID string) (*state.SourceCheckpoint, error) {
	return s.DB.GetSourceCheckpoint(ctx, typ, sourceID)
}
func (s *Service) SubmitOutlookIngestion(ctx context.Context, in state.OutlookIngestionBatchInput) (*state.OutlookIngestionBatch, error) {
	return s.DB.CreateOutlookIngestionBatch(ctx, in)
}
func (s *Service) UpsertOutlookIngestionItem(ctx context.Context, in state.OutlookIngestionItemInput) (*state.OutlookIngestionItem, bool, error) {
	return s.DB.UpsertOutlookIngestionItem(ctx, in)
}
func (s *Service) EnqueueOutlookOutbox(ctx context.Context, batchID, itemID, key string) (*state.OutlookOutbox, error) {
	return s.DB.EnqueueOutlookOutbox(ctx, batchID, itemID, key)
}
func (s *Service) ClaimOutlookOutbox(ctx context.Context, owner string, lease time.Duration) (*state.OutlookOutbox, error) {
	return s.DB.ClaimOutlookOutbox(ctx, owner, lease)
}
func (s *Service) CompleteOutlookOutbox(ctx context.Context, outboxID, owner, receiptState, resultJSON string) error {
	return s.DB.CompleteOutlookOutboxAtomically(ctx, outboxID, owner, receiptState, resultJSON)
}
func (s *Service) FailOutlookOutbox(ctx context.Context, outboxID, owner, errorText string, retryAt time.Time) error {
	return s.DB.FailOutlookOutbox(ctx, outboxID, owner, errorText, retryAt)
}
func (s *Service) RecordOutlookReceipt(ctx context.Context, batchID, itemID, receiptState, resultJSON, errText string) (*state.OutlookIngestionReceipt, error) {
	return s.DB.RecordOutlookIngestionReceipt(ctx, batchID, itemID, receiptState, resultJSON, errText)
}
func (s *Service) SetOutlookItemState(ctx context.Context, itemID, itemState, errText string) error {
	return s.DB.SetOutlookIngestionItemState(ctx, itemID, itemState, errText)
}
func (s *Service) SetOutlookBatchState(ctx context.Context, batchID, batchState, errText string) error {
	return s.DB.SetOutlookIngestionBatchState(ctx, batchID, batchState, errText)
}
func (s *Service) TriggerCloudAgentRun(ctx context.Context, in state.AgentRunTriggerInput) (*state.AgentRunTrigger, error) {
	return s.DB.TriggerAgentRun(ctx, in)
}
func (s *Service) UpsertAgentTaskCheckpoint(ctx context.Context, in state.AgentTaskCheckpointInput) (*state.AgentTaskCheckpoint, error) {
	return s.DB.UpsertAgentTaskCheckpoint(ctx, in)
}
