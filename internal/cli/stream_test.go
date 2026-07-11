package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func TestResolveStreamCompatibilityFlags(t *testing.T) {
	if got, _ := resolveStreamMode("pretty", false, true, false); got != streaming.ModeEvents {
		t.Fatalf("events-only mode=%q", got)
	}
	if got, _ := resolveStreamMode("raw", true, false, false); got != streaming.ModeOff {
		t.Fatalf("no-stream mode=%q", got)
	}
	if got, _ := resolveStreamMode("raw", false, false, true); got != streaming.ModeOff {
		t.Fatalf("json mode=%q", got)
	}
	if got, _ := resolveStreamMode("jsonl", false, false, true); got != streaming.ModeJSONL {
		t.Fatalf("jsonl mode with --json=%q", got)
	}
}

func TestEpicJSONLStreamContractAndReconnect(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "init", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	runCLI(t, dir, "pack", "install", "--json")
	runCLI(t, dir, "harness", "sync", "--yes", "--json")
	created := runCLI(t, dir, "epic", "add", "--title", "Machine stream", "--body", "Verify JSONL", "--yes", "--json")
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(created), &envelope); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VES_RUNNER_MODE", "stub")
	stream := runCLI(t, dir, "run", "epic", envelope.Data.ID, "--stream=jsonl", "--stop-after", "ticketize", "--idempotency-key", "stream-contract", "--yes", "--json")
	records := decodeProtocolRecords(t, stream)
	if len(records) < 2 || records[0].Kind != "event" || records[len(records)-1].Kind != "result" {
		t.Fatalf("records=%#v", records)
	}
	result := records[len(records)-1]
	if result.OK == nil || !*result.OK || result.RunID == "" {
		t.Fatalf("result=%#v", result)
	}
	lastSeq := records[len(records)-2].Seq
	watched := runCLI(t, dir, "run", "watch", result.RunID, "--jsonl", "--after-seq", fmt.Sprint(lastSeq))
	watchedRecords := decodeProtocolRecords(t, watched)
	if len(watchedRecords) != 1 || watchedRecords[0].Kind != "result" {
		t.Fatalf("watch records=%#v", watchedRecords)
	}
	replayed := runCLI(t, dir, "run", "epic", envelope.Data.ID, "--stream=jsonl", "--idempotency-key", "stream-contract", "--yes")
	replayRecords := decodeProtocolRecords(t, replayed)
	if len(replayRecords) != 1 || replayRecords[0].Kind != "result" || replayRecords[0].RunID != result.RunID {
		t.Fatalf("replay records=%#v", replayRecords)
	}
}

func decodeProtocolRecords(t *testing.T, raw string) []streaming.ProtocolRecord {
	t.Helper()
	var records []streaming.ProtocolRecord
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		var record streaming.ProtocolRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid JSONL record: %v\n%s", err, line)
		}
		if record.Schema != streaming.ProtocolSchema {
			t.Fatalf("schema=%q", record.Schema)
		}
		records = append(records, record)
	}
	return records
}

func TestEventDetailLoadsRawRecord(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".vessica", "runs", "run_test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"type":"item.completed","item":{"type":"command_execution"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	event := &state.Event{Type: "agent.activity", PayloadJSON: fmt.Sprintf(`{"detail":"tests passed","raw_log_path":".vessica/runs/run_test/agent.jsonl","raw_log_offset":0,"raw_log_length":%d}`, len(raw))}
	got := eventDetail(root, event)
	if !strings.Contains(got, "tests passed") || !strings.Contains(got, "item.completed") {
		t.Fatalf("detail=%q", got)
	}
}

func TestOptionalStreamModeArgs(t *testing.T) {
	mode := "pretty"
	cmd := newRunCmd(&App{})
	epic, _, err := cmd.Find([]string{"epic"})
	if err != nil {
		t.Fatal(err)
	}
	if err := epic.Flags().Set("stream", "pretty"); err != nil {
		t.Fatal(err)
	}
	validator := optionalStreamModeArgs(&mode)
	if err := validator(epic, []string{"epic_test", "raw"}); err != nil {
		t.Fatal(err)
	}
	if mode != "raw" {
		t.Fatalf("mode=%q", mode)
	}
}

func TestLogReplayIncludesCollapsedPromptEventID(t *testing.T) {
	event := state.Event{ID: "evt_prompt", Type: "agent.prompt", PayloadJSON: `{"message":"Prompt prepared (collapsed)","kind":"prompt"}`}
	got := formatEventLine(event, false, map[string]bool{})
	if !strings.Contains(got, "prepared (collapsed)") || !strings.Contains(got, "evt_prompt") {
		t.Fatalf("line=%q", got)
	}
}
