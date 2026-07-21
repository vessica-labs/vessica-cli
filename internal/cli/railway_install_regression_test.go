package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
)

func TestRailwaySecretsUseSingleLineJSONAndRepairLegacyHex(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	t.Setenv("HOME", home)
	t.Setenv("VES_AUTH_STORE", "file")
	want := railwaySecrets{ServiceToken: "service-token", KnowledgeToken: "knowledge-token"}
	legacy, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.StoreSecret(railwaySecretsReference(root), []byte(hex.EncodeToString(legacy))); err != nil {
		t.Fatal(err)
	}
	got, err := loadRailwaySecrets(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceToken != want.ServiceToken || got.KnowledgeToken != want.KnowledgeToken {
		t.Fatalf("credentials=%#v", got)
	}
	repaired, err := auth.LoadSecret(railwaySecretsReference(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(repaired), "\n") || !json.Valid(repaired) {
		t.Fatalf("credential record was not normalized: %q", repaired)
	}
}

func TestOptionalRailwaySecretsRejectsCorruptCredentialRecord(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	t.Setenv("HOME", home)
	t.Setenv("VES_AUTH_STORE", "file")
	if err := auth.StoreSecret(railwaySecretsReference(root), []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOptionalRailwaySecrets(root); err == nil || !strings.Contains(err.Error(), "invalid credential record") {
		t.Fatalf("error=%v", err)
	}
}

func TestOptionalRailwaySecretsAllowsMissingCredentialRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("VES_AUTH_STORE", "file")
	got, err := loadOptionalRailwaySecrets(filepath.Join(home, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if got != (railwaySecrets{}) {
		t.Fatalf("credentials=%#v", got)
	}
}

func TestInitializeRailwaySecretsPreservesRetainedValues(t *testing.T) {
	retained := railwaySecrets{
		ServiceToken: "service", WorkerToken: "worker", PreviewEdgeToken: "preview-edge", WebhookSecret: "webhook", CredentialKey: "credential",
		KnowledgeToken: "knowledge", KnowledgeAdminToken: "knowledge-admin", AgentRuntimeToken: "agent-runtime",
		ControlDatabasePassword: "control-db", KnowledgeDatabasePassword: "knowledge-db",
	}
	got := initializeRailwaySecrets(retained, "runtime")
	if got.ServiceToken != "service" || got.WorkerToken != "worker" || got.PreviewEdgeToken != "preview-edge" || got.KnowledgeToken != "knowledge" || got.AgentRuntimeToken != "agent-runtime" || got.ControlDatabasePassword != "control-db" {
		t.Fatalf("retained credentials were rotated: %#v", got)
	}
	if got.RuntimeToken != "runtime" {
		t.Fatalf("runtime token=%q", got.RuntimeToken)
	}
	empty := initializeRailwaySecrets(railwaySecrets{}, "runtime")
	if empty.ServiceToken == "" || empty.WorkerToken == "" || empty.PreviewEdgeToken == "" || empty.KnowledgeToken == "" || empty.AgentRuntimeToken == "" || empty.ControlDatabasePassword == "" {
		t.Fatalf("credentials were not initialized: %#v", empty)
	}
}

func TestConfigureRailwayControlPlaneMigrationAddsPreDeployCommand(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "commands.log")
	stdinPath := filepath.Join(home, "stdin.json")
	if err := os.WriteFile(filepath.Join(bin, "railway"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VES_TEST_COMMAND_LOG\"\ncat > \"$VES_TEST_STDIN_LOG\"\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("VES_TEST_STDIN_LOG", stdinPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Config{Hosted: config.HostedConfig{ProjectID: "project", EnvironmentID: "production", ServiceID: "control-plane"}}
	if err := configureRailwayControlPlaneMigration(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	command, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "environment edit --project project --environment production --message Configure Vessica database migration --json\n"
	if string(command) != want {
		t.Fatalf("command=%q want=%q", command, want)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	var patch map[string]any
	if err := json.Unmarshal(stdin, &patch); err != nil {
		t.Fatalf("invalid config patch: %v: %s", err, stdin)
	}
	services := patch["services"].(map[string]any)
	service := services["control-plane"].(map[string]any)
	deploy := service["deploy"].(map[string]any)
	if deploy["preDeployCommand"] != "ves control-plane migrate" {
		t.Fatalf("migration config=%#v", patch)
	}
}

func TestConfigureRailwayControlPlaneImageAttachesSourceAfterMigration(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(home, "commands.log")
	if err := os.WriteFile(filepath.Join(bin, "railway"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VES_TEST_COMMAND_LOG\"\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_TEST_COMMAND_LOG", logPath)
	t.Setenv("RAILWAY_TOKEN", "test-token")
	cfg := config.Config{Hosted: config.HostedConfig{ProjectID: "project", EnvironmentID: "production", ServiceID: "control-plane"}}
	if err := configureRailwayControlPlaneImage(context.Background(), cfg, "ghcr.io/vessica-labs/vessica-cli@sha256:abc"); err != nil {
		t.Fatal(err)
	}
	command, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "service source connect --project project --environment production --service control-plane --image ghcr.io/vessica-labs/vessica-cli@sha256:abc --json\n"
	if string(command) != want {
		t.Fatalf("command=%q want=%q", command, want)
	}
}

func TestOrientationCloneRemoteUsesHTTPSForGitHubSSH(t *testing.T) {
	cases := map[string]string{
		"git@github.com:vessica-labs/vessica.git":       "https://github.com/vessica-labs/vessica.git",
		"ssh://git@github.com/vessica-labs/vessica.git": "https://github.com/vessica-labs/vessica.git",
		"https://github.com/vessica-labs/vessica.git":   "https://github.com/vessica-labs/vessica.git",
		"git@example.com:team/repo.git":                 "git@example.com:team/repo.git",
	}
	for remote, want := range cases {
		if got := orientationCloneRemote(remote); got != want {
			t.Errorf("orientationCloneRemote(%q)=%q want=%q", remote, got, want)
		}
	}
}

func TestResumeRetriesFailedRepositoryMapping(t *testing.T) {
	op := onboarding.New("onb-test", "https://github.com/example/repo.git")
	op.Set("repository_mapping", "failed", "clone failed")
	if !resumeRequiresRepositoryMap(op.ID, op) {
		t.Fatal("failed repository mapping was not retained for resume")
	}
	if resumeRequiresRepositoryMap("", op) {
		t.Fatal("non-resume operation unexpectedly forced a repository map")
	}
	op.Set("repository_mapping", "succeeded", "mapped")
	if resumeRequiresRepositoryMap(op.ID, op) {
		t.Fatal("successful repository mapping was retried")
	}
}

func TestAttachedRailwayUpOptionsPreserveOperationalConfiguration(t *testing.T) {
	cfg := config.Config{
		Hosted: config.HostedConfig{WorkspaceID: "workspace-id", WorkspaceName: "workspace-name", WorkerCheckpoint: "checkpoint"},
		Tracker: config.TrackerConfig{
			Provider:       "linear",
			TeamID:         "team",
			ProjectID:      "project",
			TodoStateID:    "todo",
			WIPStateID:     "wip",
			DoneStateID:    "done",
			BlockedStateID: "blocked",
			TriggerLabel:   "Vessica",
		},
	}

	opts := attachedRailwayUpOptions(cfg, "", "", nil)
	if opts.Workspace != "workspace-id" || opts.WorkspaceName != "workspace-name" || opts.WorkerCheckpoint != "checkpoint" {
		t.Fatalf("hosted options were not preserved: %#v", opts)
	}
	if !opts.EnableLinear || opts.Team != "team" || opts.LinearProject != "project" || opts.TodoState != "todo" || opts.WIPState != "wip" || opts.DoneState != "done" || opts.BlockedState != "blocked" || opts.TriggerLabel != "Vessica" {
		t.Fatalf("tracker options were not preserved: %#v", opts)
	}
}

func TestSyncHostedKnowledgeCredentialsUpdatesLocalTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := config.Config{Knowledge: config.KnowledgeConfig{Endpoint: "https://knowledge.example"}}
	if err := syncHostedKnowledgeCredentials(cfg, railwaySecrets{KnowledgeToken: "read-token", KnowledgeAdminToken: "export-token"}); err != nil {
		t.Fatal(err)
	}
	readToken, err := auth.Token("knowledge")
	if err != nil || readToken != "read-token" {
		t.Fatalf("knowledge token=%q err=%v", readToken, err)
	}
	exportToken, err := auth.Token("knowledge-export")
	if err != nil || exportToken != "export-token" {
		t.Fatalf("knowledge export token=%q err=%v", exportToken, err)
	}
}
