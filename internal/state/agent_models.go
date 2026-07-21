package state

type Agent struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	Name           string `json:"name"`
	Purpose        string `json:"purpose"`
	State          string `json:"state"`
	CurrentVersion int    `json:"current_version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}
type AgentVersion struct {
	AgentID        string `json:"agent_id"`
	Version        int    `json:"version"`
	DefinitionJSON string `json:"definition_json"`
	ProvenanceJSON string `json:"provenance_json"`
	CreatedAt      string `json:"created_at"`
}
type AgentBuildOperation struct {
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	AgentID      string `json:"agent_id,omitempty"`
	Kind         string `json:"kind"`
	Description  string `json:"description"`
	Review       bool   `json:"review"`
	Status       string `json:"status"`
	WarningsJSON string `json:"warnings_json"`
	UsageJSON    string `json:"usage_json"`
	Error        string `json:"error,omitempty"`
	CreatedBy    string `json:"created_by"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}
type AgentDraft struct {
	ID             string `json:"id"`
	OperationID    string `json:"operation_id"`
	WorkspaceID    string `json:"workspace_id"`
	AgentID        string `json:"agent_id,omitempty"`
	DefinitionJSON string `json:"definition_json"`
	WarningsJSON   string `json:"warnings_json"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	ActivatedAt    string `json:"activated_at,omitempty"`
}
type AgentRun struct {
	ID                      string `json:"id"`
	WorkspaceID             string `json:"workspace_id"`
	AgentID                 string `json:"agent_id"`
	DefinitionVersion       int    `json:"definition_version"`
	Trigger                 string `json:"trigger"`
	InputJSON               string `json:"input_json"`
	OriginatingRepositoryID string `json:"originating_repository_id,omitempty"`
	ParentRunID             string `json:"parent_run_id,omitempty"`
	RootRunID               string `json:"root_run_id"`
	NestingDepth            int    `json:"nesting_depth"`
	Status                  string `json:"status"`
	BudgetPeriodStart       string `json:"budget_period_start"`
	ReservationMicroUSD     int64  `json:"reservation_microusd"`
	RateSnapshotJSON        string `json:"rate_snapshot_json"`
	ResolvedKnowledgeJSON   string `json:"resolved_knowledge_json"`
	OutputJSON              string `json:"output_json,omitempty"`
	TerminalError           string `json:"terminal_error,omitempty"`
	CancelRequestedAt       string `json:"cancel_requested_at,omitempty"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
	StartedAt               string `json:"started_at,omitempty"`
	FinishedAt              string `json:"finished_at,omitempty"`
}
type AgentRunAttempt struct {
	ID            string `json:"id"`
	RunID         string `json:"run_id"`
	AttemptNumber int    `json:"attempt_number"`
	WorkerID      string `json:"worker_id"`
	FenceToken    string `json:"-"`
	Status        string `json:"status"`
	LeaseUntil    string `json:"lease_until"`
	HeartbeatAt   string `json:"heartbeat_at"`
	UsageJSON     string `json:"usage_json"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
}
type AgentRunEvent struct {
	ID             string `json:"id"`
	RunID          string `json:"run_id"`
	AttemptID      string `json:"attempt_id"`
	Seq            int64  `json:"seq"`
	AttemptOrdinal int64  `json:"attempt_ordinal"`
	Type           string `json:"type"`
	PayloadJSON    string `json:"payload_json"`
	CreatedAt      string `json:"created_at"`
}
type AgentRuntimeTask struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Kind        string `json:"kind"`
	SubjectID   string `json:"subject_id"`
	Status      string `json:"status"`
	AvailableAt string `json:"available_at"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	LeaseOwner  string `json:"lease_owner,omitempty"`
	LeaseUntil  string `json:"lease_until,omitempty"`
	FenceToken  string `json:"-"`
	PayloadJSON string `json:"payload_json"`
	LastError   string `json:"last_error,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type AgentSchedule struct {
	AgentID   string `json:"agent_id"`
	Enabled   bool   `json:"enabled"`
	Cron      string `json:"cron"`
	Timezone  string `json:"timezone"`
	NextDueAt string `json:"next_due_at,omitempty"`
	LastDueAt string `json:"last_due_at,omitempty"`
	UpdatedAt string `json:"updated_at"`
}
