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
func (s *Service) ValidateOAuthRefreshToken(ctx context.Context, materialHash, resource string) (*state.OAuthRefreshToken, error) {
	return s.DB.GetOAuthRefreshToken(ctx, materialHash, resource)
}
func (s *Service) ExchangeOAuthAuthorizationCode(ctx context.Context, codeHash, clientID, redirectURI, codeChallenge, resource string) (*state.OAuthAuthorizationCode, error) {
	return s.DB.ExchangeOAuthAuthorizationCode(ctx, codeHash, clientID, redirectURI, codeChallenge, resource)
}
func (s *Service) ValidateOAuthAccessToken(ctx context.Context, tokenHash, resource string) (*state.OAuthAccessToken, error) {
	return s.DB.GetOAuthAccessToken(ctx, tokenHash, resource)
}
func (s *Service) ConsumeOAuthRefreshToken(ctx context.Context, tokenHash, clientID, resource string) (*state.OAuthRefreshToken, error) {
	return s.DB.ConsumeOAuthRefreshToken(ctx, tokenHash, clientID, resource)
}
func (s *Service) RecordAction(ctx context.Context, in state.ActionLedgerInput) (*state.ActionLedger, error) {
	return s.DB.AppendActionLedger(ctx, in)
}
func (s *Service) ClaimAction(ctx context.Context, in state.ActionLedgerInput, lease time.Duration) (*state.ActionExecutionClaim, error) {
	return s.DB.ClaimActionExecution(ctx, in, lease)
}
func (s *Service) CompleteAction(ctx context.Context, ledgerID, claimToken, resultJSON string, latencyMS int64) error {
	return s.DB.CompleteActionExecution(ctx, ledgerID, claimToken, resultJSON, latencyMS)
}
func (s *Service) FailAction(ctx context.Context, ledgerID, claimToken, resultJSON string) error {
	return s.DB.FailActionExecution(ctx, ledgerID, claimToken, resultJSON)
}
func (s *Service) StartConversation(ctx context.Context, in state.ConversationInput) (*state.Conversation, error) {
	return s.DB.CreateConversation(ctx, in)
}
func (s *Service) AddConversationMessage(ctx context.Context, conversationID string, in state.ConversationMessageInput) (*state.ConversationMessage, error) {
	return s.DB.AppendConversationMessage(ctx, conversationID, in)
}
func (s *Service) SendConversationMessageIdempotent(ctx context.Context, actionKey, actorID, conversationID, title string, in state.ConversationMessageInput) (*state.Conversation, *state.ConversationMessage, bool, error) {
	return s.DB.SendConversationMessageIdempotent(ctx, actionKey, actorID, conversationID, title, in)
}
func (s *Service) ConversationMessages(ctx context.Context, conversationID string, after int64) ([]state.ConversationMessage, error) {
	return s.DB.ListConversationMessages(ctx, conversationID, after)
}
func (s *Service) UpsertNewsletterSubscription(ctx context.Context, in state.NewsletterSubscriptionInput) (*state.NewsletterSubscription, error) {
	return s.DB.UpsertNewsletterSubscription(ctx, in)
}
func (s *Service) NewsletterSubscriptions(ctx context.Context) ([]state.NewsletterSubscription, error) {
	return s.DB.ListNewsletterSubscriptions(ctx)
}
func (s *Service) DisableNewsletterSubscription(ctx context.Context, ref string) (*state.NewsletterSubscription, error) {
	return s.DB.DisableNewsletterSubscription(ctx, ref)
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
func (s *Service) ReserveOutlookIngestion(ctx context.Context, in state.OutlookIngestionBatchInput, emailExpected, emailCandidate, calendarExpected, calendarCandidate string) (*state.OutlookIngestionBatch, error) {
	return s.DB.CreateOutlookIngestionBatchWithCheckpoints(ctx, in, emailExpected, emailCandidate, calendarExpected, calendarCandidate)
}
func (s *Service) UpsertOutlookIngestionItem(ctx context.Context, in state.OutlookIngestionItemInput) (*state.OutlookIngestionItem, bool, error) {
	return s.DB.UpsertOutlookIngestionItem(ctx, in)
}
func (s *Service) AcceptOutlookIngestionItem(ctx context.Context, in state.OutlookIngestionItemInput, processingKey string) (*state.OutlookIngestionItem, *state.OutlookOutbox, bool, error) {
	return s.DB.UpsertOutlookIngestionItemAndEnqueue(ctx, in, processingKey)
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
func (s *Service) FinalizeOutlookIngestion(ctx context.Context, batchID, emailExpected, emailCandidate, emailCheckpoint, calendarExpected, calendarCandidate, calendarCheckpoint string, reservationToken ...string) error {
	return s.DB.FinalizeOutlookIngestionBatch(ctx, batchID, emailExpected, emailCandidate, emailCheckpoint, calendarExpected, calendarCandidate, calendarCheckpoint, reservationToken...)
}
func (s *Service) TriggerCloudAgentRun(ctx context.Context, in state.AgentRunTriggerInput) (*state.AgentRunTrigger, error) {
	return s.DB.TriggerAgentRun(ctx, in)
}
func (s *Service) AgentRuns(ctx context.Context, agentID string) ([]state.AgentRun, error) {
	return s.DB.ListAgentRuns(ctx, agentID)
}
func (s *Service) AgentRunsForWorkspace(ctx context.Context, workspaceID, agentID string) ([]state.AgentRun, error) {
	return s.DB.ListAgentRunsForWorkspace(ctx, workspaceID, agentID)
}
func (s *Service) AgentRun(ctx context.Context, runID string) (*state.AgentRun, error) {
	return s.DB.GetAgentRun(ctx, runID)
}
func (s *Service) AgentRunForWorkspace(ctx context.Context, workspaceID, runID string) (*state.AgentRun, error) {
	return s.DB.GetAgentRunForWorkspace(ctx, workspaceID, runID)
}
func (s *Service) UpsertAgentTaskCheckpoint(ctx context.Context, in state.AgentTaskCheckpointInput) (*state.AgentTaskCheckpoint, error) {
	return s.DB.UpsertAgentTaskCheckpoint(ctx, in)
}
