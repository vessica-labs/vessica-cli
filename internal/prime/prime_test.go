package prime

import (
	"context"
	"strings"
	"testing"
)

func TestBuildHostedContextWithoutLocalDB(t *testing.T) {
	resp, err := Build(context.Background(), nil, t.TempDir(), Request{For: "codex", WorkspaceID: "ws_hosted"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Workspace != "ws_hosted" {
		t.Fatalf("workspace=%q", resp.Workspace)
	}
}

func TestBuildHostedContextRejectsStateSelectors(t *testing.T) {
	_, err := Build(context.Background(), nil, t.TempDir(), Request{WorkspaceID: "ws_hosted", EpicID: "epic_1"})
	if err == nil || !strings.Contains(err.Error(), "does not yet support") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrimeCommandsEngineManagedExcludeLifecycle(t *testing.T) {
	commands := strings.Join(primeCommands(true), "\n")
	for _, forbidden := range []string{"ves ticket claim", "ves ticket close", "ves memory add", "ves ticket heartbeat"} {
		if strings.Contains(commands, forbidden) {
			t.Fatalf("engine-managed commands contained %q: %s", forbidden, commands)
		}
	}
}

func TestPrimeRulesEngineManagedForbidLifecycle(t *testing.T) {
	rules := strings.Join(primeRules(true), "\n")
	if !strings.Contains(rules, "Do not run ves ticket claim") {
		t.Fatalf("engine-managed rules did not forbid lifecycle commands: %s", rules)
	}
	if !strings.Contains(rules, "return a concise evidence summary") {
		t.Fatalf("engine-managed rules did not ask for evidence summary: %s", rules)
	}
}
