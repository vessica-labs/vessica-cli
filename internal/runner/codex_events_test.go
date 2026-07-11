package runner

import "testing"

func TestCodexEventParserCommandAndMessage(t *testing.T) {
	p := newCodexEventParser()
	started := p.parse(`{"type":"item.started","item":{"id":"item_0","type":"command_execution","command":"pnpm test","aggregated_output":"","exit_code":null,"status":"in_progress"}}`, "coder")
	if started.Type != "agent.activity" || started.Data["kind"] != "command" || started.Data["activity_id"] != "item_0" {
		t.Fatalf("started=%#v", started)
	}
	completed := p.parse(`{"type":"item.completed","item":{"id":"item_0","type":"command_execution","command":"pnpm test","aggregated_output":"2 tests passed","exit_code":0,"status":"completed"}}`, "coder")
	if completed.Message != "pnpm test" || completed.Data["detail"] != "2 tests passed" {
		t.Fatalf("completed=%#v", completed)
	}
	message := p.parse(`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Implemented the feature."}}`, "coder")
	if message.Type != "agent.message" || message.Message != "Implemented the feature." {
		t.Fatalf("message=%#v", message)
	}
}

func TestFileChangeSummaryHidesPatchBody(t *testing.T) {
	command := "apply_patch <<'PATCH'\n*** Begin Patch\n*** Update File: internal/app.go\n@@\n-secret body\n*** End Patch\nPATCH"
	if got := fileChangeSummary(command); got != "internal/app.go" {
		t.Fatalf("got=%q", got)
	}
}

func TestCodexEventParserUsage(t *testing.T) {
	p := newCodexEventParser()
	event := p.parse(`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20}}`, "planner")
	if event.Type != "agent.usage" || event.Data["input_tokens"] != float64(100) {
		t.Fatalf("event=%#v", event)
	}
}

func TestClassifyCommand(t *testing.T) {
	for command, want := range map[string]string{
		"rg -n TODO .":          "search",
		"sed -n '1,20p' x":      "file_read",
		"apply_patch <<'PATCH'": "file_change",
		"go test ./...":         "command",
	} {
		if got := classifyCommand(command); got != want {
			t.Fatalf("%q: got=%q want=%q", command, got, want)
		}
	}
}
