package state

import "time"

func Now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

type Workspace struct {
	ID        string `json:"id"`
	RootPath  string `json:"root_path"`
	Profile   string `json:"profile"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Repository struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	Provider        string `json:"provider"`
	CanonicalRemote string `json:"canonical_remote"`
	Remote          string `json:"remote"`
	DisplayName     string `json:"display_name"`
	DefaultBranch   string `json:"default_branch"`
	Status          string `json:"status"`
	MetadataJSON    string `json:"metadata_json"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type Epic struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	RepositoryID string `json:"repository_id"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Status       string `json:"status"`
	ExternalID   string `json:"external_id,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type Artifact struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	RepositoryID    string `json:"repository_id"`
	EpicID          string `json:"epic_id,omitempty"`
	ArtifactSetID   string `json:"artifact_set_id,omitempty"`
	Type            string `json:"type"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Version         int    `json:"version"`
	Body            string `json:"body"`
	FrontmatterJSON string `json:"frontmatter_json"`
	SourceRunID     string `json:"source_run_id,omitempty"`
	CreatedByJSON   string `json:"created_by_json"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type ArtifactSet struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	EpicID          string `json:"epic_id"`
	Status          string `json:"status"`
	ArtifactIDsJSON string `json:"artifact_ids_json"`
	SourceRunID     string `json:"source_run_id,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type Ticket struct {
	ID                  string   `json:"id"`
	WorkspaceID         string   `json:"workspace_id"`
	EpicID              string   `json:"epic_id"`
	SourceRunID         string   `json:"source_run_id,omitempty"`
	WaveID              string   `json:"wave_id,omitempty"`
	Type                string   `json:"type"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	Status              string   `json:"status"`
	EvidenceReceiptID   string   `json:"evidence_receipt_id,omitempty"`
	DiscoveredFromRunID string   `json:"discovered_from_run_id,omitempty"`
	TestStep            string   `json:"test_step,omitempty"`
	ExternalID          string   `json:"external_id,omitempty"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	DependsOn           []string `json:"depends_on,omitempty"`
}

type Wave struct {
	ID          string `json:"id"`
	EpicID      string `json:"epic_id"`
	SourceRunID string `json:"source_run_id,omitempty"`
	Index       int    `json:"index"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Claim struct {
	ID          string `json:"id"`
	TicketID    string `json:"ticket_id"`
	AgentID     string `json:"agent_id"`
	LeaseUntil  string `json:"lease_until"`
	HeartbeatAt string `json:"heartbeat_at"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type Run struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	RepositoryID     string `json:"repository_id"`
	EpicID           string `json:"epic_id,omitempty"`
	TicketID         string `json:"ticket_id,omitempty"`
	Workflow         string `json:"workflow"`
	Status           string `json:"status"`
	CurrentPhase     string `json:"current_phase,omitempty"`
	StartPhase       string `json:"start_phase,omitempty"`
	StopAfter        string `json:"stop_after,omitempty"`
	Concurrency      int    `json:"concurrency"`
	Runner           string `json:"runner,omitempty"`
	Model            string `json:"model,omitempty"`
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
	SandboxBackend   string `json:"sandbox_backend,omitempty"`
	SandboxID        string `json:"sandbox_id,omitempty"`
	SandboxExpiresAt string `json:"sandbox_expires_at,omitempty"`
	Preview          bool   `json:"preview"`
	PRMode           string `json:"pr_mode"`
	PreviewURL       string `json:"preview_url,omitempty"`
	PRURL            string `json:"pr_url,omitempty"`
	ReceiptID        string `json:"receipt_id,omitempty"`
	ArtifactSetID    string `json:"artifact_set_id,omitempty"`
	Error            string `json:"error,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	StartedAt        string `json:"started_at,omitempty"`
	FinishedAt       string `json:"finished_at,omitempty"`
}

type RunPhase struct {
	RunID      string `json:"run_id"`
	Phase      string `json:"phase"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Sandbox struct {
	ID             string `json:"id"`
	RunID          string `json:"run_id,omitempty"`
	WorkspaceID    string `json:"workspace_id"`
	Backend        string `json:"backend"`
	ContainerID    string `json:"container_id,omitempty"`
	Status         string `json:"status"`
	Branch         string `json:"branch,omitempty"`
	PreviewPort    int    `json:"preview_port,omitempty"`
	PreviewURL     string `json:"preview_url,omitempty"`
	MetaJSON       string `json:"meta_json"`
	LastAccessedAt string `json:"last_accessed_at,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	RetainedUntil  string `json:"retained_until,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	DestroyedAt    string `json:"destroyed_at,omitempty"`
}

type Event struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id,omitempty"`
	SandboxID   string `json:"sandbox_id,omitempty"`
	Seq         int64  `json:"seq"`
	Type        string `json:"type"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   string `json:"created_at"`
}

type RunEvidence struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	Phase       string `json:"phase"`
	Kind        string `json:"kind"`
	TicketID    string `json:"ticket_id,omitempty"`
	Status      string `json:"status"`
	BodyJSON    string `json:"body_json"`
	CreatedAt   string `json:"created_at"`
}

type Receipt struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	EpicID      string `json:"epic_id,omitempty"`
	Status      string `json:"status"`
	BodyJSON    string `json:"body_json"`
	CreatedAt   string `json:"created_at"`
}

type Trace struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	WorkspaceID string `json:"workspace_id"`
	Summary     string `json:"summary"`
	BodyJSON    string `json:"body_json"`
	CreatedAt   string `json:"created_at"`
}

type ExternalMapping struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	RepositoryID string `json:"repository_id,omitempty"`
	Provider     string `json:"provider"`
	EntityType   string `json:"entity_type"`
	LocalID      string `json:"local_id"`
	ExternalID   string `json:"external_id"`
	MetaJSON     string `json:"meta_json"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type TrackerIntegration struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	Provider     string `json:"provider"`
	Status       string `json:"status"`
	ConfigJSON   string `json:"config_json"`
	WebhookID    string `json:"webhook_id,omitempty"`
	SecretRef    string `json:"secret_ref,omitempty"`
	LastSyncedAt string `json:"last_synced_at,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type WebhookDelivery struct {
	ID            string `json:"id"`
	IntegrationID string `json:"integration_id"`
	Provider      string `json:"provider"`
	DeliveryID    string `json:"delivery_id"`
	EventType     string `json:"event_type"`
	PayloadJSON   string `json:"payload_json"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	LastError     string `json:"last_error,omitempty"`
	CreatedAt     string `json:"created_at"`
	ProcessedAt   string `json:"processed_at,omitempty"`
}

type Job struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	RepositoryID string `json:"repository_id,omitempty"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	PayloadJSON  string `json:"payload_json"`
	RunID        string `json:"run_id,omitempty"`
	Attempts     int    `json:"attempts"`
	MaxAttempts  int    `json:"max_attempts"`
	LeaseOwner   string `json:"lease_owner,omitempty"`
	LeaseUntil   string `json:"lease_until,omitempty"`
	AvailableAt  string `json:"available_at"`
	LastError    string `json:"last_error,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type OutboxMessage struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	IntegrationID  string `json:"integration_id"`
	Operation      string `json:"operation"`
	IdempotencyKey string `json:"idempotency_key"`
	PayloadJSON    string `json:"payload_json"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	AvailableAt    string `json:"available_at"`
	LastError      string `json:"last_error,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ControlPlaneDeployment struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	Provider          string `json:"provider"`
	ProjectID         string `json:"project_id"`
	EnvironmentID     string `json:"environment_id"`
	ServiceID         string `json:"service_id"`
	PostgresServiceID string `json:"postgres_service_id,omitempty"`
	PublicURL         string `json:"public_url,omitempty"`
	Version           string `json:"version,omitempty"`
	Status            string `json:"status"`
	MetaJSON          string `json:"meta_json"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type PackRecord struct {
	ID          string `json:"id"`
	Ref         string `json:"ref"`
	Origin      string `json:"origin,omitempty"`
	Version     string `json:"version,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	InstalledAt string `json:"installed_at"`
	Pinned      bool   `json:"pinned"`
}

type HarnessStatus struct {
	WorkspaceID      string `json:"workspace_id"`
	LastSyncAt       string `json:"last_sync_at,omitempty"`
	DriftStatus      string `json:"drift_status"`
	PackVersion      string `json:"pack_version,omitempty"`
	MissingFilesJSON string `json:"missing_files_json"`
	UpdatedAt        string `json:"updated_at"`
}
