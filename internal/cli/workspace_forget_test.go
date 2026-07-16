package cli

import (
	"os"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
)

func TestWorkspaceForgetRemovesOnlyLocalHostedAttachment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	root := t.TempDir()
	runCLI(t, root, "dev", "up", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hosted = config.HostedConfig{Provider: "railway", WorkspaceID: "workspace", ProjectID: "project", ControlPlaneURL: "https://control.example"}
	cfg.Attachment = config.AttachmentConfig{WorkspaceID: "workspace", RepositoryID: "repository"}
	cfg.Knowledge = config.KnowledgeConfig{Mode: "hosted", Endpoint: "https://knowledge.example"}
	if err := onboarding.SaveInstallation(cfg, []byte(`{"APIToken":"installation-token"}`)); err != nil {
		t.Fatal(err)
	}
	if err := saveRailwaySecrets(root, railwaySecrets{APIToken: "repository-token"}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}

	runCLI(t, root, "workspace", "forget", "--yes", "--json")

	forgotten, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if forgotten.Hosted.ProjectID != "" || forgotten.Attachment.RepositoryID != "" || forgotten.Knowledge.Mode != "local" {
		t.Fatalf("hosted attachment remains after forget: %#v", forgotten)
	}
	if _, _, err := onboarding.FindInstallation("workspace"); !os.IsNotExist(err) {
		t.Fatalf("installation registry entry remains: %v", err)
	}
	if _, err := loadRailwaySecrets(root); err == nil {
		t.Fatal("repository Railway credentials remain after forget")
	}
}
