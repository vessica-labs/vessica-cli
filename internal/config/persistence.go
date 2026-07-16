package config

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type repositoryAttachmentDescriptor struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Workspace  struct {
		ID       string `yaml:"id"`
		Endpoint string `yaml:"endpoint"`
		Provider string `yaml:"provider"`
	} `yaml:"workspace"`
	Repository struct {
		ID     string `yaml:"id"`
		Remote string `yaml:"remote"`
	} `yaml:"repository"`
	Harness struct {
		Pack    string `yaml:"pack"`
		Version string `yaml:"version"`
	} `yaml:"harness"`
}

// Load reads workspace config and merges non-secret hosted metadata from the
// user registry when the committed file is a repository attachment.
func Load(root string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(Path(root))
	if err != nil {
		return c, err
	}
	var attachment repositoryAttachmentDescriptor
	if err := yaml.Unmarshal(b, &attachment); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	if attachment.Kind == "RepositoryAttachment" && attachment.Workspace.ID != "" && attachment.Repository.ID != "" {
		if known, found := loadKnownInstallation(attachment.Workspace.Endpoint); found {
			c = known
		}
		c.APIVersion = firstNonEmptyConfig(attachment.APIVersion, "vessica.dev/v1")
		c.Kind = attachment.Kind
		c.Attachment = AttachmentConfig{WorkspaceID: attachment.Workspace.ID, RepositoryID: attachment.Repository.ID}
		c.Hosted.Provider = attachment.Workspace.Provider
		c.Hosted.ControlPlaneURL = attachment.Workspace.Endpoint
		c.Repo.Remote = attachment.Repository.Remote
		return c, nil
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

// Save writes either development config or the minimal committed attachment.
func Save(root string, c Config) error {
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		return err
	}
	var value any = c
	if c.Kind == "RepositoryAttachment" && c.Attachment.WorkspaceID != "" && c.Attachment.RepositoryID != "" && c.Hosted.ControlPlaneURL != "" {
		descriptor := repositoryAttachmentDescriptor{APIVersion: firstNonEmptyConfig(c.APIVersion, "vessica.dev/v1"), Kind: "RepositoryAttachment"}
		descriptor.Workspace.ID, descriptor.Workspace.Endpoint, descriptor.Workspace.Provider = c.Attachment.WorkspaceID, c.Hosted.ControlPlaneURL, c.Hosted.Provider
		descriptor.Repository.ID, descriptor.Repository.Remote = c.Attachment.RepositoryID, c.Repo.Remote
		descriptor.Harness.Pack, descriptor.Harness.Version = "@vessica/engineering-harness", "v1.0.0"
		value = descriptor
	}
	b, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(root), b, 0o644)
}

func loadKnownInstallation(endpoint string) (Config, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, false
	}
	path := filepath.Join(home, ".vessica", "client.db")
	if _, err := os.Stat(path); err != nil {
		return Config{}, false
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return Config{}, false
	}
	defer db.Close()
	var configJSON string
	if err := db.QueryRow(`SELECT config_json FROM installations WHERE lower(rtrim(endpoint,'/'))=lower(rtrim(?,'/'))`, endpoint).Scan(&configJSON); err != nil {
		return Config{}, false
	}
	var cfg Config
	if json.Unmarshal([]byte(configJSON), &cfg) != nil {
		return Config{}, false
	}
	return cfg, true
}

func firstNonEmptyConfig(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
