package state

type OAuthClientInput struct{ ClientID, Name, RedirectURIsJSON, ScopesJSON, SecretHash string }
type OAuthClient struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	ClientID         string `json:"client_id"`
	Name             string `json:"name"`
	RedirectURIsJSON string `json:"redirect_uris_json"`
	ScopesJSON       string `json:"scopes_json"`
	SecretHash       string `json:"-"`
	RevokedAt        string `json:"revoked_at,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}
type OAuthAuthorizationCodeInput struct{ ClientID, ActorID, CodeHash, RedirectURI, ScopesJSON, ExpiresAt string }
type OAuthAuthorizationCode struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ClientID    string `json:"client_id"`
	ActorID     string `json:"actor_id"`
	CodeHash    string `json:"-"`
	RedirectURI string `json:"redirect_uri"`
	ScopesJSON  string `json:"scopes_json"`
	ExpiresAt   string `json:"expires_at"`
	ConsumedAt  string `json:"consumed_at,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}
type OAuthAccessTokenInput struct{ ClientID, ActorID, TokenHash, ScopesJSON, ExpiresAt string }
type OAuthAccessToken struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ClientID    string `json:"client_id"`
	ActorID     string `json:"actor_id"`
	TokenHash   string `json:"-"`
	ScopesJSON  string `json:"scopes_json"`
	ExpiresAt   string `json:"expires_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}
type OAuthRefreshTokenInput struct{ ClientID, ActorID, MaterialHash, FamilyID, ScopesJSON, ExpiresAt string }
type OAuthRefreshToken struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	ClientID     string `json:"client_id"`
	ActorID      string `json:"actor_id"`
	MaterialHash string `json:"-"`
	FamilyID     string `json:"family_id"`
	ScopesJSON   string `json:"scopes_json"`
	ExpiresAt    string `json:"expires_at"`
	ReplacedAt   string `json:"replaced_at,omitempty"`
	RevokedAt    string `json:"revoked_at,omitempty"`
	CreatedAt    string `json:"created_at"`
}

type ActionLedgerInput struct {
	ActorID, AgentID, AgentRunID, Tool, PolicyDecision string
	RedactedArgumentsJSON, ResultJSON                  string
	LatencyMS                                          int64
	IdempotencyKey, ExternalIDsJSON                    string
}
type ActionLedger struct {
	ID, WorkspaceID, ActorID, AgentID, AgentRunID, Tool, PolicyDecision string
	RedactedArgumentsJSON, ResultJSON                                   string
	LatencyMS                                                           int64
	IdempotencyKey, ExternalIDsJSON, CreatedAt                          string
}
type ConversationInput struct{ ActorID, Title string }
type Conversation struct{ ID, WorkspaceID, ActorID, Title, Status, CreatedAt, UpdatedAt string }
type ConversationMessageInput struct{ Role, ContentJSON, MetadataJSON string }
type ConversationMessage struct {
	ID, ConversationID, WorkspaceID            string
	Sequence                                   int64
	Role, ContentJSON, MetadataJSON, CreatedAt string
}
type SourceCheckpoint struct{ WorkspaceID, SourceType, SourceID, CheckpointJSON, UpdatedAt string }
type NewsletterSubscriptionInput struct {
	SourceKey, SourceURL, Title, Status, MetadataJSON string
	RetentionDays                                     int
}
type NewsletterSubscription struct {
	ID, WorkspaceID, SourceKey, SourceURL, Title, Status string
	RetentionDays                                        int
	MetadataJSON, CreatedAt, UpdatedAt                   string
}
type NewsletterItemInput struct{ SubscriptionID, SourceItemID, NormalizedJSON, PublishedAt, RetainUntil string }
type NewsletterItem struct{ ID, WorkspaceID, SubscriptionID, SourceItemID, NormalizedJSON, PublishedAt, RetainUntil, CreatedAt, UpdatedAt string }
type OutlookIngestionBatchInput struct{ IdempotencyKey, SubmittedBy, CheckpointJSON, WarningsJSON string }
type OutlookIngestionBatch struct{ ID, WorkspaceID, IdempotencyKey, SubmittedBy, State, CheckpointJSON, WarningsJSON, Error, CreatedAt, UpdatedAt, CompletedAt string }
type OutlookIngestionItemInput struct{ BatchID, SourceID, InternetMessageID, ConversationID, MessageAt, NormalizedJSON string }
type OutlookIngestionItem struct{ ID, WorkspaceID, BatchID, SourceID, InternetMessageID, ConversationID, MessageAt, NormalizedJSON, State, Error, CreatedAt, UpdatedAt string }
type OutlookIngestionReceipt struct{ ID, WorkspaceID, BatchID, ItemID, State, ResultJSON, Error, CreatedAt string }
type OutlookOutbox struct {
	ID, WorkspaceID, BatchID, ItemID, ProcessingKey, State, LeaseOwner, LeaseUntil, AvailableAt, ProcessedAt, LastError, CreatedAt, UpdatedAt string
	Attempts                                                                                                                                  int
}
type AgentTaskCheckpointInput struct{ AgentID, AgentRunID, CheckpointKey, StateJSON, Status string }
type AgentTaskCheckpoint struct{ ID, WorkspaceID, AgentID, AgentRunID, CheckpointKey, StateJSON, Status, CreatedAt, UpdatedAt string }
type AgentRunTriggerInput struct {
	AgentID, IdempotencyKey, Trigger, InputJSON, RepositoryID, ParentRunID string
	RateSnapshot                                                           any
}
type AgentRunTrigger struct{ ID, WorkspaceID, AgentID, IdempotencyKey, Trigger, InputJSON, RepositoryID, ParentRunID, AgentRunID, State, ClaimToken, LeaseUntil, RateSnapshotJSON, CreatedAt, UpdatedAt string }
