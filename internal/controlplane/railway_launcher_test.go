package controlplane

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func TestRailwayWorkerBootstrapUsesOuterSandbox(t *testing.T) {
	script := railwayWorkerBootstrap("https://control.example/internal/worker/ves", "run_test", "")
	if !strings.Contains(script, "export VES_CODEX_EXTERNAL_SANDBOX=1") {
		t.Fatalf("bootstrap does not configure Codex for the Railway isolation boundary:\n%s", script)
	}
	if !strings.Contains(script, "worker_bin=$(mktemp ") || strings.Contains(script, "-o /tmp/ves\n") {
		t.Fatalf("bootstrap does not download workers atomically:\n%s", script)
	}
	for _, required := range []string{"export HOME=/home/vessica-agent", "export NPM_CONFIG_PREFIX=/usr/local", "export NODE_PATH=/usr/local/lib/node_modules", "export PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright", "useradd --create-home", "auth.json", "chown vessica-agent:vessica-agent", "chmod 0600", "codex login status", "VES_BOOTSTRAP_STARTED_AT_MS", "VES_TOOLCHAIN_VERIFIED_AT_MS", "VES_WORKER_DOWNLOADED_AT_MS", "command -v runuser", "command -v find", toolchain.YQVersion, "command -v", "runuser --user vessica-agent"} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap is missing toolchain preflight %q:\n%s", required, script)
		}
	}
	for _, forbidden := range []string{"npm install -g", "playwright install", "npm view @openai/codex version", "@openai/codex@latest", "playwright@latest", "deb.nodesource.com", "pnpm run build", "pnpm test", "127.0.0.1:4173", "chromium.launch"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap contains mutable dependency %q:\n%s", forbidden, script)
		}
	}
	command := exec.Command("bash", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap shell is invalid: %v: %s", err, output)
	}
}

func TestRailwayResumeBootstrapIncludesPhase(t *testing.T) {
	script := railwayWorkerBootstrap("https://control.example/internal/worker/ves", "run_test", "validate")
	for _, required := range []string{"control-plane worker", "--run-id 'run_test'", "--from 'validate'"} {
		if !strings.Contains(script, required) {
			t.Fatalf("resume bootstrap missing %q:\n%s", required, script)
		}
	}
}

func TestRailwayPromptBootstrapEncodesPrompt(t *testing.T) {
	prompt := "Make the CTA smaller; don't change the scheduler."
	script := railwayPromptBootstrap("https://control.example/internal/worker/ves", "run_test", prompt)
	if strings.Contains(script, prompt) {
		t.Fatal("prompt should not be embedded as plain shell text")
	}
	for _, required := range []string{"control-plane prompt-worker", "--run-id 'run_test'", "--prompt-b64"} {
		if !strings.Contains(script, required) {
			t.Fatalf("prompt bootstrap missing %q:\n%s", required, script)
		}
	}
}

func TestRailwayWorkerEnvironmentIncludesHostedKnowledgeAuthority(t *testing.T) {
	launcher := &RailwayLauncher{Config: config.Config{Hosted: config.HostedConfig{WorkerCheckpoint: "checkpoint"}}}
	env := launcher.workerEnvironment("run_test", "https://github.com/acme/demo.git", "repository-checkpoint", time.Unix(100, 0))
	for _, key := range []string{"VES_KNOWLEDGE_MODE", "VES_KNOWLEDGE_ENDPOINT", "VES_KNOWLEDGE_TOKEN", "VES_KNOWLEDGE_WORKSPACE_ID"} {
		if env[key] != "control-plane."+key {
			t.Fatalf("%s=%q", key, env[key])
		}
	}
	if env["VES_CONTROL_DATABASE_URL"] != "control-plane.VES_CONTROL_DATABASE_URL" {
		t.Fatalf("VES_CONTROL_DATABASE_URL=%q", env["VES_CONTROL_DATABASE_URL"])
	}
	if _, ok := env["VES_KNOWLEDGE_DATABASE_URL"]; ok {
		t.Fatal("worker must not receive the knowledge database URL")
	}
	if env["VES_RAILWAY_CHECKPOINT"] != "repository-checkpoint" || env["VES_SANDBOX_REQUESTED_AT_MS"] == "" {
		t.Fatalf("checkpoint environment=%#v", env)
	}
}

func TestRailwayWorkerEnvironmentPrefersDedicatedWorkerDatabaseURL(t *testing.T) {
	t.Setenv("VES_CONTROL_DATABASE_WORKER_URL", "postgresql://public-proxy.example/database")
	launcher := &RailwayLauncher{Config: config.Config{Hosted: config.HostedConfig{WorkerCheckpoint: "checkpoint"}}}
	env := launcher.workerEnvironment("run_test", "https://github.com/acme/demo.git", "repository-checkpoint", time.Unix(100, 0))
	if env["VES_CONTROL_DATABASE_URL"] != "control-plane.VES_CONTROL_DATABASE_WORKER_URL" {
		t.Fatalf("VES_CONTROL_DATABASE_URL=%q", env["VES_CONTROL_DATABASE_URL"])
	}
}

func TestRailwayLauncherPrefersCompatibleRepositoryCheckpoint(t *testing.T) {
	checkpoint := reposnapshot.Checkpoint{SchemaVersion: reposnapshot.SchemaVersion, Name: "vessica-repo-ready", Status: "ready", ToolchainFingerprint: toolchain.Fingerprint()}
	metadata, _ := json.Marshal(map[string]any{"repository_checkpoint": checkpoint})
	launcher := &RailwayLauncher{Config: config.Config{Hosted: config.HostedConfig{WorkerCheckpoint: "toolchain"}}}
	name, kind, _ := launcher.resolveCheckpoint(&state.Repository{MetadataJSON: string(metadata)})
	if name != checkpoint.Name || kind != "repository" {
		t.Fatalf("name=%s kind=%s", name, kind)
	}
	checkpoint.ToolchainFingerprint = "stale"
	metadata, _ = json.Marshal(map[string]any{"repository_checkpoint": checkpoint})
	name, kind, _ = launcher.resolveCheckpoint(&state.Repository{MetadataJSON: string(metadata)})
	if name != "toolchain" || kind != "toolchain" {
		t.Fatalf("stale name=%s kind=%s", name, kind)
	}
}
