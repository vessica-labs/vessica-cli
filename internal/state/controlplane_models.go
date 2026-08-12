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
type OAuthAuthorizationCodeInput struct {
	ClientID, ActorID, CodeHash, RedirectURI, ScopesJSON, ExpiresAt string
	CodeChallenge, CodeChallengeMethod                              string
	Resource                                                        string
}
type OAuthAuthorizationCode struct {
	ID                  string `json:"id"`
	WorkspaceID         string `json:"workspace_id"`
	ClientID            string `json:"client_id"`
	ActorID             string `json:"actor_id"`
	CodeHash            string `json:"-"`
	RedirectURI         string `json:"redirect_uri"`
	ScopesJSON          string `json:"scopes_json"`
	CodeChallenge       string `json:"-"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Resource            string `json:"resource"`
	ExpiresAt           string `json:"expires_at"`
	ConsumedAt          string `json:"consumed_at,omitempty"`
	RevokedAt           string `json:"revoked_at,omitempty"`
	CreatedAt           string `json:"created_at"`
}
type OAuthAccessTokenInput struct{ ClientID, ActorID, TokenHash, FamilyID, Resource, ScopesJSON, ExpiresAt string }
type OAuthAccessToken struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ClientID    string `json:"client_id"`
	ActorID     string `json:"actor_id"`
	TokenHash   string `json:"-"`
	FamilyID    string `json:"-"`
	Resource    string `json:"resource"`
	ScopesJSON  string `json:"scopes_json"`
	ExpiresAt   string `json:"expires_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}
type OAuthRefreshTokenInput struct{ ClientID, ActorID, MaterialHash, FamilyID, Resource, ScopesJSON, ExpiresAt string }
type OAuthRefreshToken struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	ClientID     string `json:"client_id"`
	ActorID      string `json:"actor_id"`
	MaterialHash string `json:"-"`
	FamilyID     string `json:"family_id"`
	Resource     string `json:"resource"`
	ScopesJSON   string `json:"scopes_json"`
	ExpiresAt    string `json:"expires_at"`
	ReplacedAt   string `json:"replaced_at,omitempty"`
	RevokedAt    string `json:"revoked_at,omitempty"`
	CreatedAt    string `json:"created_at"`
}

type ActionLedgerInput struct {
	ActorID, AgentID, AgentRunID, Tool, PolicyDecision string
	RedactedArgumentsJSON, ResultJSON                  string
	ArgumentsHash                                      string
	LatencyMS                                          int64
	IdempotencyKey, ExternalIDsJSON                    string
}
type ActionLedger struct {
	ID, WorkspaceID, ActorID, AgentID, AgentRunID, Tool, PolicyDecision string
	RedactedArgumentsJSON, ResultJSON                                   string
	LatencyMS                                                           int64
	IdempotencyKey, ExternalIDsJSON, CreatedAt                          string
	ExecutionState                                                      string
	ClaimTokenHash                                                      string `json:"-"`
	ArgumentsHash                                                       string `json:"-"`
	LeaseUntil                                                          string `json:"-"`
	UpdatedAt                                                           string
}
type ActionExecutionClaim struct {
	Ledger     *ActionLedger
	ClaimToken string `json:"-"`
	Acquired   bool
	Replay     bool
}
type ConversationInput struct{ ActorID, AgentID, Title string }
type Conversation struct{ ID, WorkspaceID, ActorID, AgentID, Title, Status, CreatedAt, UpdatedAt string }
type ConversationMessageInput struct{ Role, ContentJSON, MetadataJSON string }
type ConversationMessage struct {
	ID, ConversationID, WorkspaceID            string
	Sequence                                   int64
	Role, ContentJSON, MetadataJSON, CreatedAt string
}
type SourceCheckpoint struct{ WorkspaceID, SourceType, SourceID, CheckpointJSON, CheckpointValue, UpdatedAt string }
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
type OutlookIngestionBatch struct {
	ID, WorkspaceID, IdempotencyKey, SubmittedBy, State, CheckpointJSON, WarningsJSON, Error, CreatedAt, UpdatedAt, CompletedAt string
	ReservationToken                                                                                                            string `json:"-"`
}
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
type AgentRunTrigger struct {
	ID               string `json:"-"`
	WorkspaceID      string `json:"-"`
	AgentID          string `json:"agent_id"`
	IdempotencyKey   string `json:"-"`
	Trigger          string `json:"trigger"`
	InputJSON        string `json:"-"`
	RepositoryID     string `json:"repository_id,omitempty"`
	ParentRunID      string `json:"parent_run_id,omitempty"`
	AgentRunID       string `json:"run_id"`
	State            string `json:"state"`
	ClaimToken       string `json:"-"`
	LeaseUntil       string `json:"-"`
	RateSnapshotJSON string `json:"-"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type CloudOrchestrationTask struct {
	ID, WorkspaceID, Kind, SubjectID, State, PayloadJSON, RunID, ArtifactID string
	Attempts                                                                int
	AvailableAt, LeaseOwner, LeaseUntil, LastError                          string
	CreatedAt, UpdatedAt, CompletedAt                                       string
}
