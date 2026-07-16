package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseChangedFiles(t *testing.T) {
	got := parseChangedFiles(" M index.html\n?? scripts/check.mjs\nR  old.txt -> new.txt\n")
	want := []string{"index.html", "scripts/check.mjs", "new.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCodexRunnerStreamsStructuredJSONEvents(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
out=""
printf '%s\n' "$@" > "$CODEX_ARGS_FILE"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then
    shift
    out="$1"
  fi
  shift
done
printf '%s\n' '{"type":"thread.started","thread_id":"thread_test"}'
printf '%s\n' '{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"go test ./...","status":"in_progress"}}'
printf '%s\n' '{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"go test ./...","aggregated_output":"ok","exit_code":0,"status":"completed"}}'
printf '%s\n' '{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Implemented and tested."}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":12,"output_tokens":4}}'
printf '%s\n' 'Implemented and tested.' > "$out"
`
	path := filepath.Join(bin, "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VES_RUNNER_MODE", "")
	t.Setenv("VES_SIMULATION", "")
	t.Setenv("VES_CODEX_EXTERNAL_SANDBOX", "1")
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("CODEX_ARGS_FILE", argsFile)

	r := NewCodex()
	workdir := t.TempDir()
	if err := r.Prepare(context.Background(), Input{RepoPath: workdir, Workdir: workdir, Env: map[string]string{"CODEX_ARGS_FILE": argsFile}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background(), Task{Name: "coder", Prompt: "Do it"}); err != nil {
		t.Fatal(err)
	}
	events, err := r.StreamEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for event := range events {
		types = append(types, event.Type)
	}
	result, err := r.CollectResult(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || result.Output != "Implemented and tested." {
		t.Fatalf("result=%#v", result)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("args=%s", args)
	}
	joined := strings.Join(types, ",")
	for _, typ := range []string{"agent.activity", "agent.message", "agent.usage"} {
		if !strings.Contains(joined, typ) {
			t.Fatalf("events=%v missing %s", types, typ)
		}
	}
}

func TestRunnerEnvironmentDoesNotInheritControlPlaneSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")
	t.Setenv("VES_KNOWLEDGE_TOKEN", "knowledge-secret")
	t.Setenv("OPENAI_API_KEY", "model-secret")
	t.Setenv("VES_RUNNER_HOME", "/tmp/agent-home")
	env := strings.Join(runnerEnvironment(map[string]string{"VES_RUN_ID": "run_test"}), "\n")
	for _, forbidden := range []string{"DATABASE_URL=", "GITHUB_TOKEN=", "VES_KNOWLEDGE_TOKEN="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("runner inherited %s: %s", forbidden, env)
		}
	}
	for _, required := range []string{"OPENAI_API_KEY=model-secret", "HOME=/tmp/agent-home", "VES_RUN_ID=run_test"} {
		if !strings.Contains(env, required) {
			t.Fatalf("runner environment missing %s: %s", required, env)
		}
	}
}
