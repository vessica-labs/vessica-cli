package onboarding

import (
	"os"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestInstallationRegistryKeepsCredentialsOutOfRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VES_AUTH_STORE", "file")
	cfg := config.Defaults()
	cfg.Hosted.WorkspaceID = "railway-workspace"
	cfg.Hosted.ProjectID = "railway-project"
	cfg.Hosted.ControlPlaneURL = "https://control.example"
	secret := []byte(`{"APIToken":"top-secret"}`)
	if err := SaveInstallation(cfg, secret); err != nil {
		t.Fatal(err)
	}
	path, err := installationsPath()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" || strings.Contains(string(body), "top-secret") {
		t.Fatalf("registry leaked credential: %s", body)
	}
	installation, loaded, err := FindInstallation("railway-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if installation.ProjectID != "railway-project" || string(loaded) != string(secret) {
		t.Fatalf("installation=%#v credentials=%s", installation, loaded)
	}
}
