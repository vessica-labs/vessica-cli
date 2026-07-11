package controlplane

import (
	"strings"
	"testing"
)

func TestRailwayWorkerBootstrapUsesOuterSandbox(t *testing.T) {
	script := railwayWorkerBootstrap("https://control.example/internal/worker/ves", "run_test")
	if !strings.Contains(script, "export VES_CODEX_EXTERNAL_SANDBOX=1") {
		t.Fatalf("bootstrap does not configure Codex for the Railway isolation boundary:\n%s", script)
	}
	if !strings.Contains(script, "worker_bin=$(mktemp ") || strings.Contains(script, "-o /tmp/ves\n") {
		t.Fatalf("bootstrap does not download workers atomically:\n%s", script)
	}
	for _, required := range []string{"export NODE_PATH=$(npm root -g)", "npm install -g playwright@latest", "playwright install --with-deps chromium"} {
		if !strings.Contains(script, required) {
			t.Fatalf("bootstrap is missing managed Playwright setup %q:\n%s", required, script)
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
