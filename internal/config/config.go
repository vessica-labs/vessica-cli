package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DirName      = ".vessica"
	ConfigFile   = "config.yaml"
	PackLockFile = "pack.lock"
	HarnessFile  = "harness.yaml"
)

// Config is workspace configuration.
type Config struct {
	APIVersion string           `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Kind       string           `yaml:"kind,omitempty" json:"kind,omitempty"`
	State      StateConfig      `yaml:"state" json:"state"`
	Sandbox    SandboxConfig    `yaml:"sandbox" json:"sandbox"`
	Runner     RunnerConfig     `yaml:"runner" json:"runner"`
	Repo       RepoConfig       `yaml:"repo" json:"repo"`
	Tracker    TrackerConfig    `yaml:"tracker" json:"tracker"`
	Hosted     HostedConfig     `yaml:"hosted,omitempty" json:"hosted,omitempty"`
	Knowledge  KnowledgeConfig  `yaml:"knowledge" json:"knowledge"`
	Pack       PackConfig       `yaml:"pack" json:"pack"`
	Preview    PreviewConfig    `yaml:"preview" json:"preview"`
	Attachment AttachmentConfig `yaml:"attachment,omitempty" json:"attachment,omitempty"`
}

type AttachmentConfig struct {
	WorkspaceID  string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	RepositoryID string `yaml:"repository_id,omitempty" json:"repository_id,omitempty"`
}

type StateConfig struct {
	Backend string `yaml:"backend" json:"backend"` // sqlite | postgres-url | postgres-docker
	DBURL   string `yaml:"db_url" json:"db_url"`
}

type SandboxConfig struct {
	Backend string `yaml:"backend" json:"backend"` // docker | railway | local
}

type RunnerConfig struct {
	Default         string `yaml:"default" json:"default"`                   // codex | claude | cursor | pi
	Model           string `yaml:"model" json:"model"`                       // Codex model ID
	ReasoningEffort string `yaml:"reasoning_effort" json:"reasoning_effort"` // low | medium | high | xhigh
}

type RepoConfig struct {
	Provider string `yaml:"provider" json:"provider"` // github | gitlab
	Remote   string `yaml:"remote" json:"remote"`
}

type TrackerConfig struct {
	Provider       string `yaml:"provider" json:"provider"` // linear | jira | none
	Mode           string `yaml:"mode" json:"mode"`
	TeamID         string `yaml:"team_id,omitempty" json:"team_id,omitempty"`
	TodoStateID    string `yaml:"todo_state_id,omitempty" json:"todo_state_id,omitempty"`
	WIPStateID     string `yaml:"wip_state_id,omitempty" json:"wip_state_id,omitempty"`
	DoneStateID    string `yaml:"done_state_id,omitempty" json:"done_state_id,omitempty"`
	BlockedStateID string `yaml:"blocked_state_id,omitempty" json:"blocked_state_id,omitempty"`
	TriggerLabel   string `yaml:"trigger_label,omitempty" json:"trigger_label,omitempty"`
}

type HostedConfig struct {
	Provider          string `yaml:"provider,omitempty" json:"provider,omitempty"`
	WorkspaceID       string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	ProjectID         string `yaml:"project_id,omitempty" json:"project_id,omitempty"`
	EnvironmentID     string `yaml:"environment_id,omitempty" json:"environment_id,omitempty"`
	ServiceID         string `yaml:"service_id,omitempty" json:"service_id,omitempty"`
	PostgresServiceID string `yaml:"postgres_service_id,omitempty" json:"postgres_service_id,omitempty"`
	ControlPlaneURL   string `yaml:"control_plane_url,omitempty" json:"control_plane_url,omitempty"`
	ControlPlaneImage string `yaml:"control_plane_image,omitempty" json:"control_plane_image,omitempty"`
	WorkerCheckpoint  string `yaml:"worker_checkpoint,omitempty" json:"worker_checkpoint,omitempty"`
}

type KnowledgeConfig struct {
	Mode              string `yaml:"mode" json:"mode"`
	WorkspaceID       string `yaml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	Endpoint          string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	LocalPath         string `yaml:"local_path,omitempty" json:"local_path,omitempty"`
	ServiceID         string `yaml:"service_id,omitempty" json:"service_id,omitempty"`
	Version           string `yaml:"version,omitempty" json:"version,omitempty"`
	Image             string `yaml:"image,omitempty" json:"image,omitempty"`
	EmbeddingProvider string `yaml:"embedding_provider,omitempty" json:"embedding_provider,omitempty"`
	EmbeddingModel    string `yaml:"embedding_model,omitempty" json:"embedding_model,omitempty"`
}

type PackConfig struct {
	Lockfile string `yaml:"lockfile" json:"lockfile"`
}

type PreviewConfig struct {
	Command     string `yaml:"command" json:"command"`
	Port        int    `yaml:"port" json:"port"`
	Healthcheck string `yaml:"healthcheck" json:"healthcheck"`
}

// Defaults returns local developer defaults. Product onboarding uses
// HostedDefaults so repository attachments never inherit a writable local
// state or knowledge backend.
func Defaults() Config {
	return Config{
		APIVersion: "vessica.dev/v1",
		Kind:       "RepositoryAttachment",
		State:      StateConfig{Backend: "sqlite"},
		Sandbox:    SandboxConfig{Backend: "docker"},
		Runner:     RunnerConfig{Default: "codex", Model: "gpt-5.6-terra", ReasoningEffort: "high"},
		Repo:       RepoConfig{Provider: "github"},
		Tracker:    TrackerConfig{Provider: "none", Mode: "best_efforts"},
		Knowledge:  KnowledgeConfig{Mode: "local", LocalPath: filepath.Join(DirName, "state", "knowledge.db")},
		Pack:       PackConfig{Lockfile: filepath.Join(DirName, PackLockFile)},
		Preview:    PreviewConfig{Port: 3000},
	}
}

// HostedDefaults returns the transient configuration used while creating a
// hosted installation. It intentionally contains no repository-local storage
// backend; hosted product state becomes available only through the control
// plane and knowledge service.
func HostedDefaults() Config {
	c := Defaults()
	c.State = StateConfig{Backend: "hosted"}
	c.Sandbox.Backend = "railway"
	c.Tracker = TrackerConfig{Provider: "none", Mode: "best_efforts"}
	c.Knowledge = KnowledgeConfig{Mode: "hosted"}
	return c
}

// IsHostedAttachment reports whether the configuration points at the hosted
// product authority rather than a local developer datastore.
func IsHostedAttachment(c Config) bool {
	return c.Kind == "RepositoryAttachment" && c.Hosted.ControlPlaneURL != "" && c.Attachment.RepositoryID != ""
}

// EnforceHostedAuthority removes local runtime backends from a repository
// attachment after registry and environment overlays have been applied.
func EnforceHostedAuthority(c *Config) {
	if !IsHostedAttachment(*c) {
		return
	}
	c.State = StateConfig{Backend: "hosted"}
	c.Sandbox.Backend = "railway"
	c.Knowledge.Mode = "hosted"
	c.Knowledge.LocalPath = ""
}

// TeamDefaults returns team-profile defaults.
func TeamDefaults() Config {
	c := Defaults()
	c.State.Backend = "postgres-url"
	c.Tracker.Provider = "linear"
	return c
}

// FindRoot walks up from cwd looking for .vessica/config.yaml.
func FindRoot(cwd string) (string, error) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	for {
		cfg := filepath.Join(dir, DirName, ConfigFile)
		if st, err := os.Stat(cfg); err == nil && !st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not a vessica workspace (no %s/%s found)", DirName, ConfigFile)
		}
		dir = parent
	}
}

// Dir returns the .vessica directory for a workspace root.
func Dir(root string) string {
	return filepath.Join(root, DirName)
}

// Path returns the config file path.
func Path(root string) string {
	return filepath.Join(root, DirName, ConfigFile)
}

// Get returns a dotted key value as string.
func Get(c Config, key string) (string, error) {
	m := flatten(c)
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("unknown config key: %s", key)
	}
	return v, nil
}

// Set updates a dotted key.
func Set(c *Config, key, value string) error {
	switch key {
	case "state.backend":
		c.State.Backend = value
	case "state.db_url":
		c.State.DBURL = value
	case "sandbox.backend":
		c.Sandbox.Backend = value
	case "runner.default":
		c.Runner.Default = value
	case "repo.provider":
		c.Repo.Provider = value
	case "repo.remote":
		c.Repo.Remote = value
	case "tracker.provider":
		c.Tracker.Provider = value
	case "tracker.mode":
		c.Tracker.Mode = value
	case "tracker.team_id":
		c.Tracker.TeamID = value
	case "tracker.todo_state_id":
		c.Tracker.TodoStateID = value
	case "tracker.wip_state_id":
		c.Tracker.WIPStateID = value
	case "tracker.done_state_id":
		c.Tracker.DoneStateID = value
	case "tracker.blocked_state_id":
		c.Tracker.BlockedStateID = value
	case "tracker.trigger_label":
		c.Tracker.TriggerLabel = value
	case "hosted.provider":
		c.Hosted.Provider = value
	case "hosted.workspace_id":
		c.Hosted.WorkspaceID = value
	case "hosted.project_id":
		c.Hosted.ProjectID = value
	case "hosted.environment_id":
		c.Hosted.EnvironmentID = value
	case "hosted.service_id":
		c.Hosted.ServiceID = value
	case "hosted.postgres_service_id":
		c.Hosted.PostgresServiceID = value
	case "hosted.control_plane_url":
		c.Hosted.ControlPlaneURL = value
	case "hosted.control_plane_image":
		c.Hosted.ControlPlaneImage = value
	case "hosted.worker_checkpoint":
		c.Hosted.WorkerCheckpoint = value
	case "knowledge.mode":
		c.Knowledge.Mode = value
	case "knowledge.workspace_id":
		c.Knowledge.WorkspaceID = value
	case "knowledge.endpoint":
		c.Knowledge.Endpoint = value
	case "knowledge.local_path":
		c.Knowledge.LocalPath = value
	case "knowledge.service_id":
		c.Knowledge.ServiceID = value
	case "knowledge.version":
		c.Knowledge.Version = value
	case "knowledge.image":
		c.Knowledge.Image = value
	case "pack.lockfile":
		c.Pack.Lockfile = value
	case "preview.command":
		c.Preview.Command = value
	case "preview.port":
		var n int
		if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
			return fmt.Errorf("preview.port must be int")
		}
		c.Preview.Port = n
	case "preview.healthcheck":
		c.Preview.Healthcheck = value
	default:
		return fmt.Errorf("unknown or unsettable config key: %s", key)
	}
	return nil
}

// Unset clears a dotted key to default-ish empty.
func Unset(c *Config, key string) error {
	switch key {
	case "state.db_url":
		c.State.DBURL = ""
	case "repo.remote":
		c.Repo.Remote = ""
	case "preview.command":
		c.Preview.Command = ""
	case "preview.healthcheck":
		c.Preview.Healthcheck = ""
	default:
		return fmt.Errorf("cannot unset key: %s", key)
	}
	return nil
}

// Flatten returns all keys for list.
func Flatten(c Config) map[string]string {
	return flatten(c)
}

func flatten(c Config) map[string]string {
	return map[string]string{
		"state.backend":              c.State.Backend,
		"state.db_url":               c.State.DBURL,
		"sandbox.backend":            c.Sandbox.Backend,
		"runner.default":             c.Runner.Default,
		"repo.provider":              c.Repo.Provider,
		"repo.remote":                c.Repo.Remote,
		"tracker.provider":           c.Tracker.Provider,
		"tracker.mode":               c.Tracker.Mode,
		"tracker.team_id":            c.Tracker.TeamID,
		"tracker.todo_state_id":      c.Tracker.TodoStateID,
		"tracker.wip_state_id":       c.Tracker.WIPStateID,
		"tracker.done_state_id":      c.Tracker.DoneStateID,
		"tracker.blocked_state_id":   c.Tracker.BlockedStateID,
		"tracker.trigger_label":      c.Tracker.TriggerLabel,
		"hosted.provider":            c.Hosted.Provider,
		"hosted.workspace_id":        c.Hosted.WorkspaceID,
		"hosted.project_id":          c.Hosted.ProjectID,
		"hosted.environment_id":      c.Hosted.EnvironmentID,
		"hosted.service_id":          c.Hosted.ServiceID,
		"hosted.postgres_service_id": c.Hosted.PostgresServiceID,
		"hosted.control_plane_url":   c.Hosted.ControlPlaneURL,
		"hosted.control_plane_image": c.Hosted.ControlPlaneImage,
		"hosted.worker_checkpoint":   c.Hosted.WorkerCheckpoint,
		"knowledge.mode":             c.Knowledge.Mode,
		"knowledge.workspace_id":     c.Knowledge.WorkspaceID,
		"knowledge.endpoint":         c.Knowledge.Endpoint,
		"knowledge.local_path":       c.Knowledge.LocalPath,
		"knowledge.service_id":       c.Knowledge.ServiceID,
		"knowledge.version":          c.Knowledge.Version,
		"knowledge.image":            c.Knowledge.Image,
		"pack.lockfile":              c.Pack.Lockfile,
		"preview.command":            c.Preview.Command,
		"preview.port":               fmt.Sprintf("%d", c.Preview.Port),
		"preview.healthcheck":        c.Preview.Healthcheck,
	}
}

// UserDir returns ~/.vessica for user-level secrets/config.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// ApplyEnv overlays VES_* environment variables.
func ApplyEnv(c *Config) {
	if v := os.Getenv("VES_STATE_BACKEND"); v != "" {
		c.State.Backend = v
	}
	if v := os.Getenv("VES_CONTROL_DATABASE_URL"); v != "" {
		c.State.DBURL = v
	}
	if v := os.Getenv("VES_RUNNER"); v != "" {
		c.Runner.Default = v
	}
	if v := os.Getenv("VES_RUNNER_MODEL"); v != "" {
		c.Runner.Model = v
	}
	if v := os.Getenv("VES_RUNNER_REASONING_EFFORT"); v != "" {
		c.Runner.ReasoningEffort = v
	}
	if v := os.Getenv("VES_SANDBOX"); v != "" {
		c.Sandbox.Backend = v
	}
	if v := os.Getenv("VES_REPO_PROVIDER"); v != "" {
		c.Repo.Provider = v
	}
	if v := os.Getenv("VES_REPO_REMOTE"); v != "" {
		c.Repo.Remote = v
	}
	if v := os.Getenv("VES_TRACKER_PROVIDER"); v != "" {
		c.Tracker.Provider = v
	}
	if v := os.Getenv("VES_LINEAR_TEAM_ID"); v != "" {
		c.Tracker.TeamID = v
	}
	if v := os.Getenv("VES_LINEAR_TODO_STATE_ID"); v != "" {
		c.Tracker.TodoStateID = v
	}
	if v := os.Getenv("VES_LINEAR_WIP_STATE_ID"); v != "" {
		c.Tracker.WIPStateID = v
	}
	if v := os.Getenv("VES_LINEAR_DONE_STATE_ID"); v != "" {
		c.Tracker.DoneStateID = v
	}
	if v := os.Getenv("VES_LINEAR_BLOCKED_STATE_ID"); v != "" {
		c.Tracker.BlockedStateID = v
	}
	if v := os.Getenv("VES_LINEAR_TRIGGER_LABEL"); v != "" {
		c.Tracker.TriggerLabel = v
	}
	if v := os.Getenv("VES_HOSTED_PROVIDER"); v != "" {
		c.Hosted.Provider = v
	}
	if v := os.Getenv("VES_RAILWAY_CHECKPOINT"); v != "" {
		c.Hosted.WorkerCheckpoint = v
	}
	if v := os.Getenv("RAILWAY_PROJECT_ID"); v != "" {
		c.Hosted.ProjectID = v
	}
	if v := os.Getenv("RAILWAY_ENVIRONMENT_ID"); v != "" {
		c.Hosted.EnvironmentID = v
	}
	if v := os.Getenv("RAILWAY_SERVICE_ID"); v != "" {
		c.Hosted.ServiceID = v
	}
	if v := os.Getenv("VES_RAILWAY_POSTGRES_SERVICE_ID"); v != "" {
		c.Hosted.PostgresServiceID = v
	}
	if v := os.Getenv("VES_CONTROL_PLANE_URL"); v != "" {
		c.Hosted.ControlPlaneURL = v
	}
	if v := os.Getenv("VES_KNOWLEDGE_MODE"); v != "" {
		c.Knowledge.Mode = v
	}
	if v := os.Getenv("VES_KNOWLEDGE_WORKSPACE_ID"); v != "" {
		c.Knowledge.WorkspaceID = v
	}
	if v := os.Getenv("VES_KNOWLEDGE_ENDPOINT"); v != "" {
		c.Knowledge.Endpoint = v
	}
}

// SQLitePath returns the default sqlite file path.
func SQLitePath(root string) string {
	return filepath.Join(root, DirName, "state", "vessica.db")
}

// EnsureGitignore appends .vessica ignore entries if missing.
func EnsureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	entries := []string{
		".vessica/cache/",
		".vessica/state/",
		".vessica/runs/",
		".vessica/sandboxes/",
		".vessica/secrets/",
	}
	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = string(b)
	}
	var add []string
	for _, e := range entries {
		if !strings.Contains(existing, e) {
			add = append(add, e)
		}
	}
	if len(add) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString("# Vessica\n"); err != nil {
		return err
	}
	for _, e := range add {
		if _, err := f.WriteString(e + "\n"); err != nil {
			return err
		}
	}
	return nil
}
