package controlplane

import (
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func TestRailwayWorkerBootstrapUsesOuterSandbox(t *testing.T) {
	script := railwayWorkerBootstrap("https://control.example/internal/worker/ves", "run_test")
	if !strings.Contains(script, "export VES_CODEX_EXTERNAL_SANDBOX=1") {
		t.Fatalf("bootstrap does not configure Codex for the Railway isolation boundary:\n%s", script)
	}
	if !strings.Contains(script, "worker_bin=$(mktemp ") || strings.Contains(script, "-o /tmp/ves\n") {
		t.Fatalf("bootstrap does not download workers atomically:\n%s", script)
	}
	for _, required := range []string{"export NODE_PATH=$(npm root -g)", "npm install -g playwright@" + toolchain.PlaywrightVersion, "npm install -g @openai/codex@" + toolchain.CodexVersion, "useradd --create-home", "command -v runuser", "command -v find", "playwright install --with-deps chromium"} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap is missing managed Playwright setup %q:\n%s", required, script)
		}
	}
	for _, forbidden := range []string{"npm view @openai/codex version", "@openai/codex@latest", "playwright@latest", "deb.nodesource.com"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap contains mutable dependency %q:\n%s", forbidden, script)
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
	env := launcher.workerEnvironment("run_test")
	for _, key := range []string{"VES_KNOWLEDGE_MODE", "VES_KNOWLEDGE_ENDPOINT", "VES_KNOWLEDGE_TOKEN", "VES_KNOWLEDGE_WORKSPACE_ID"} {
		if env[key] != "control-plane."+key {
			t.Fatalf("%s=%q", key, env[key])
		}
	}
}
