package config

// HostedConfig records the Railway resources and public origins that make up
// one hosted Vessica installation.
type HostedConfig struct {
	Provider              string `yaml:"provider,omitempty" json:"provider,omitempty"`
	WorkspaceID           string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	WorkspaceName         string `yaml:"workspace_name,omitempty" json:"workspace_name,omitempty"`
	ProjectID             string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	EnvironmentID         string `yaml:"environment_id,omitempty" json:"environment_id,omitempty"`
	ServiceID             string `yaml:"service_id,omitempty" json:"service_id,omitempty"`
	PreviewServiceID      string `yaml:"preview_service_id,omitempty" json:"preview_service_id,omitempty"`
	PostgresServiceID     string `yaml:"postgres_service_id,omitempty" json:"postgres_service_id,omitempty"`
	ControlPlaneURL       string `yaml:"control_plane_url,omitempty" json:"control_plane_url,omitempty"`
	PreviewURL            string `yaml:"preview_url,omitempty" json:"preview_url,omitempty"`
	ControlPlaneImage     string `yaml:"control_plane_image,omitempty" json:"control_plane_image,omitempty"`
	AgentRuntimeServiceID string `yaml:"agent_runtime_service_id,omitempty" json:"agent_runtime_service_id,omitempty"`
	AgentRuntimeImage     string `yaml:"agent_runtime_image,omitempty" json:"agent_runtime_image,omitempty"`
	AgentRuntimeVersion   string `yaml:"agent_runtime_version,omitempty" json:"agent_runtime_version,omitempty"`
	WorkerCheckpoint      string `yaml:"worker_checkpoint,omitempty" json:"worker_checkpoint,omitempty"`
}
