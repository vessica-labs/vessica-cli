package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func TestRailwayUpDryRunDoesNotCallRailway(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "railway.log")
	script := filepath.Join(bin, "railway")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VES_TEST_COMMAND_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	runCLI(t, dir, "init", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	raw := runCLI(t, dir, "railway", "up", "--name", "mvp-test", "--dry-run", "--json")
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			DryRun bool   `json:"dry_run"`
			Action string `json:"action"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("parse dry-run: %v\n%s", err, raw)
	}
	if !envelope.OK || !envelope.Data.DryRun || envelope.Data.Action != "railway.up" {
		t.Fatalf("unexpected dry-run envelope: %s", raw)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("railway command unexpectedly executed: %v", err)
	}
}

func TestResolveLinearConfigPrefersRequestedTeamAndStates(t *testing.T) {
	discovery := &tracker.LinearDiscovery{
		Teams: []tracker.LinearTeam{{ID: "one", Name: "One", Key: "ONE"}, {ID: "two", Name: "Product", Key: "PROD"}},
		States: map[string][]tracker.LinearWorkflowState{
			"two": {
				{ID: "todo-id", Name: "Todo", Type: "unstarted"},
				{ID: "wip-id", Name: "In Progress", Type: "started"},
				{ID: "done-id", Name: "Done", Type: "completed"},
			},
		},
	}
	team, states, err := resolveLinearConfig(discovery, railwayUpOptions{Team: "PROD", TodoState: "Todo", WIPState: "In Progress", DoneState: "Done"})
	if err != nil {
		t.Fatal(err)
	}
	if team.ID != "two" || states["todo"] != "todo-id" || states["wip"] != "wip-id" || states["done"] != "done-id" {
		t.Fatalf("team=%#v states=%#v", team, states)
	}
}

func TestRailwayJSONParsing(t *testing.T) {
	id, err := objectID([]byte(`{"project":{"id":"project-1"}}`))
	if err != nil || id != "project-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	domain, err := objectString([]byte(`{"data":{"domain":"ves.example"}}`), "domain")
	if err != nil || domain != "ves.example" {
		t.Fatalf("domain=%q err=%v", domain, err)
	}
}

func TestParseLatestRailwayDeployment(t *testing.T) {
	deployment, err := parseLatestRailwayDeployment([]byte(`[{"id":"deploy-new","status":"BUILDING"}]`))
	if err != nil || deployment.ID != "deploy-new" || deployment.Status != "BUILDING" {
		t.Fatalf("deployment=%#v err=%v", deployment, err)
	}
	if _, err := parseLatestRailwayDeployment([]byte(`[]`)); err == nil {
		t.Fatal("expected empty deployment error")
	}
}

func TestRandomSecretsAreUniqueAndNonEmpty(t *testing.T) {
	a, b := randomSecret(16), randomSecret(16)
	if len(a) != 32 || len(b) != 32 || a == b {
		t.Fatalf("a=%q b=%q", a, b)
	}
}

func TestRunRailwayUsesStoredOAuthToken(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(bin, "railway")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' \"$RAILWAY_API_TOKEN\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_AUTH_STORE", "file")
	t.Setenv("RAILWAY_TOKEN", "")
	t.Setenv("RAILWAY_API_TOKEN", "")
	if err := auth.SaveOAuth(&auth.OAuthCredential{Provider: "railway", ClientID: "client", TokenURL: "https://example.invalid", AccessToken: "oauth-access", ExpiresAt: time.Now().Add(time.Hour), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	output, err := runRailway(context.Background(), "", nil, "status")
	if err != nil || string(output) != "oauth-access" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestRunRailwaySSHKeysFallsBackToCLISession(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(bin, "railway")
	content := "#!/bin/sh\nif [ -n \"$RAILWAY_API_TOKEN\" ]; then echo 'Unauthorized.' >&2; exit 1; fi\nprintf 'session:%s' \"$*\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_AUTH_STORE", "file")
	t.Setenv("RAILWAY_TOKEN", "")
	t.Setenv("RAILWAY_API_TOKEN", "")
	if err := auth.SaveOAuth(&auth.OAuthCredential{Provider: "railway", ClientID: "client", TokenURL: "https://example.invalid", AccessToken: "oauth-access", ExpiresAt: time.Now().Add(time.Hour), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	output, err := runRailwaySSHKeys(context.Background(), "list", "--workspace", "workspace-1")
	if err != nil || string(output) != "session:ssh keys list --workspace workspace-1" {
		t.Fatalf("output=%q err=%v", output, err)
	}
}

func TestSetRailwayVariableDeletesEmptyValues(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "commands.log")
	script := filepath.Join(bin, "railway")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VES_TEST_COMMAND_LOG\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_AUTH_STORE", "file")
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Config{Hosted: config.HostedConfig{ProjectID: "project", EnvironmentID: "environment", ServiceID: "service"}}
	if err := setRailwayVariable(context.Background(), cfg, "RAILWAY_TOKEN", ""); err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(commands); got != "variable delete RAILWAY_TOKEN --project project -e environment -s service --json\n" {
		t.Fatalf("command=%q", got)
	}
}
