package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func TestRailwayCheckpointToolchainIsPinned(t *testing.T) {
	command := railwayCheckpointInstallCommand()
	for _, required := range []string{
		"apt-get install -y --no-install-recommends",
		"ripgrep",
		"fd-find",
		"jq",
		"bat",
		"gh",
		"go" + toolchain.GoVersion + ".linux-${go_arch}.tar.gz",
		toolchain.GoAMD64SHA256,
		toolchain.GoARM64SHA256,
		"node-v" + toolchain.NodeVersion + "-linux-${node_arch}.tar.xz",
		toolchain.NodeAMD64SHA256,
		toolchain.NodeARM64SHA256,
		"yq_linux_${yq_arch}",
		toolchain.YQAMD64SHA256,
		toolchain.YQARM64SHA256,
		"pnpm@" + toolchain.PNPMVersion,
		"@openai/codex@" + toolchain.CodexVersion,
		"playwright@" + toolchain.PlaywrightVersion,
		"PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright",
		"runuser --user vessica-agent",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("checkpoint command missing %q: %s", required, command)
		}
	}
	if strings.Contains(command, "@latest") || strings.Contains(command, "/latest/") {
		t.Fatalf("checkpoint command contains a mutable version: %s", command)
	}
}

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
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	raw := runCLI(t, dir, "railway", "up", "--dry-run", "--json")
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			DryRun bool           `json:"dry_run"`
			Action string         `json:"action"`
			Would  map[string]any `json:"would"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("parse dry-run: %v\n%s", err, raw)
	}
	if !envelope.OK || !envelope.Data.DryRun || envelope.Data.Action != "railway.up" {
		t.Fatalf("unexpected dry-run envelope: %s", raw)
	}
	if got := envelope.Data.Would["project_name"]; got != railwayControlPlaneProjectName {
		t.Fatalf("project_name=%v want=%q", got, railwayControlPlaneProjectName)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("railway command unexpectedly executed: %v", err)
	}
}

func TestCreateRailwayResourcesUsesFixedControlPlaneProjectName(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	root := filepath.Join(home, "repo")
	workDir := filepath.Join(home, "provision")
	for _, dir := range []string{bin, root, workDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(home, "commands.log")
	script := filepath.Join(bin, "railway")
	content := `#!/bin/sh
printf '%s\n' "$*" >> "$VES_TEST_COMMAND_LOG"
case "$1 $2" in
  "init --name") printf '%s' '{"project":{"id":"project-new"}}' ;;
  "service list") printf '%s' '[]' ;;
  "add --service") printf '%s' '{"service":{"id":"control-new"}}' ;;
  "add --database") printf '%s' '{"service":{"id":"postgres-new"}}' ;;
  *) printf '%s' '{}' ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Defaults()
	if err := createRailwayResources(context.Background(), workDir, root, &cfg, railwayUpOptions{Image: "ghcr.io/vessica-labs/vessica-cli@sha256:must-not-deploy-yet"}); err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(commands)), "\n")
	if len(lines) == 0 || lines[0] != "init --name "+railwayControlPlaneProjectName+" --json" {
		t.Fatalf("first Railway command=%q", firstNonEmpty(strings.Join(lines, "\n"), "<none>"))
	}
	if strings.Contains(string(commands), "add --image") || !strings.Contains(string(commands), "add --service control-plane --json") {
		t.Fatalf("control-plane source was attached before configuration: %s", commands)
	}
}

func TestLinkRailwayWorkDirUsesExistingProjectAndEnvironment(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	workDir := filepath.Join(home, "provision")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "commands.log")
	script := filepath.Join(bin, "railway")
	content := "#!/bin/sh\nprintf '%s|%s\\n' \"$PWD\" \"$*\" >> \"$VES_TEST_COMMAND_LOG\"\nprintf '{}'\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Config{Hosted: config.HostedConfig{ProjectID: "project-1", EnvironmentID: "environment-1"}}
	if err := linkRailwayWorkDir(context.Background(), workDir, cfg); err != nil {
		t.Fatal(err)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatal(err)
	}
	want := resolvedWorkDir + "|link --project project-1 --environment environment-1 --json\n"
	if string(commands) != want {
		t.Fatalf("command=%q want=%q", commands, want)
	}
}

func TestCreateRailwayResourcesAdoptsExistingCoreServices(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	workDir := filepath.Join(home, "provision")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "commands.log")
	script := filepath.Join(bin, "railway")
	content := `#!/bin/sh
printf '%s\n' "$*" >> "$VES_TEST_COMMAND_LOG"
if [ "$1" = "service" ] && [ "$2" = "list" ]; then
  printf '%s' '[{"id":"control-existing","name":"control-plane"},{"id":"postgres-existing","name":"Postgres"}]'
else
  printf '{}'
fi
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Defaults()
	cfg.Hosted.ProjectID = "project-existing"
	cfg.Hosted.EnvironmentID = "production"
	if err := createRailwayResources(context.Background(), workDir, home, &cfg, railwayUpOptions{}); err != nil {
		t.Fatal(err)
	}
	if cfg.Hosted.ServiceID != "control-existing" || cfg.Hosted.PostgresServiceID != "postgres-existing" {
		t.Fatalf("hosted config=%#v", cfg.Hosted)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(commands), "add --service") || strings.Contains(string(commands), "add --database") {
		t.Fatalf("existing resources were duplicated:\n%s", commands)
	}
}

func TestDeriveRailwayDatabaseURLsUsesSeparateDatabasesAndCredentials(t *testing.T) {
	urls, err := deriveRailwayDatabaseURLs("postgresql://postgres:admin@postgres.railway.internal:5432/railway?sslmode=disable", railwaySecrets{
		ControlDatabasePassword:   "control-secret",
		KnowledgeDatabasePassword: "knowledge-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"vessica_control_user:control-secret", "/vessica_control", "sslmode=disable"} {
		if !strings.Contains(urls.Control, required) {
			t.Fatalf("control URL missing %q", required)
		}
	}
	for _, required := range []string{"vessica_knowledge_user:knowledge-secret", "/vessica_knowledge", "sslmode=disable"} {
		if !strings.Contains(urls.Knowledge, required) {
			t.Fatalf("knowledge URL missing %q", required)
		}
	}
	if urls.Control == urls.Knowledge {
		t.Fatal("logical database URLs must remain distinct")
	}
}

func TestDatabasePasswordFromURLRequiresExpectedRole(t *testing.T) {
	raw := "postgres://vessica_control_user:secret@localhost/vessica_control"
	if got := databasePasswordFromURL(raw, controlDatabaseRole); got != "secret" {
		t.Fatalf("password=%q", got)
	}
	if got := databasePasswordFromURL(raw, knowledgeDatabaseRole); got != "" {
		t.Fatalf("unexpected password=%q", got)
	}
}

func TestResolvedTriggerLabelPreservesConfigurationAndDefaults(t *testing.T) {
	if got := resolvedTriggerLabel("", "Existing"); got != "Existing" {
		t.Fatalf("preserved label=%q", got)
	}
	if got := resolvedTriggerLabel("Requested", "Existing"); got != "Requested" {
		t.Fatalf("requested label=%q", got)
	}
	if got := resolvedTriggerLabel("", ""); got != "Vessica" {
		t.Fatalf("default label=%q", got)
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

func TestResolveLinearProjectBySlugAndTeam(t *testing.T) {
	project := tracker.LinearProject{ID: "project-id", Name: "Launch", SlugID: "launch"}
	project.Teams.Nodes = []tracker.LinearTeam{{ID: "team-id", Name: "Product", Key: "PROD"}}
	discovery := &tracker.LinearDiscovery{Projects: []tracker.LinearProject{project}}
	resolved, err := resolveLinearProject(discovery, "team-id", "launch", "")
	if err != nil || resolved.ID != "project-id" {
		t.Fatalf("project=%#v err=%v", resolved, err)
	}
	if _, err := resolveLinearProject(discovery, "other-team", "launch", ""); err == nil {
		t.Fatal("expected team mismatch")
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

func TestVerifyRailwayCLIVersionRejectsUntestedMajor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "railway")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'railway 4.9.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyRailwayCLIVersion(context.Background(), path); err == nil {
		t.Fatal("expected unsupported Railway CLI major to be rejected")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'railway 5.26.2'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyRailwayCLIVersion(context.Background(), path); err != nil {
		t.Fatalf("tested Railway CLI was rejected: %v", err)
	}
}
