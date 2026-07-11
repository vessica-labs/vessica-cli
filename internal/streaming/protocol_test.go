package streaming

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestEventRecordUsesStructuredPayload(t *testing.T) {
	event := &state.Event{
		ID:          "evt_test",
		RunID:       "run_test",
		Seq:         7,
		Type:        "agent.activity",
		PayloadJSON: `{"message":"go test ./...","status":"completed","command":"go test ./...","detail":"full command output","raw_log_path":".vessica/runs/run_test/agent.jsonl"}`,
		CreatedAt:   "2026-07-09T00:00:00Z",
	}
	record := EventRecord(event)
	payload, ok := record.Event.Payload.(map[string]any)
	if record.Schema != ProtocolSchema || record.Kind != "event" || !ok || payload["status"] != "completed" {
		t.Fatalf("record=%#v", record)
	}
	if _, present := payload["command"]; present {
		t.Fatalf("command was not collapsed: %#v", payload)
	}
	if _, present := payload["detail"]; present {
		t.Fatalf("detail was not collapsed: %#v", payload)
	}
	if payload["raw_log_path"] == nil || payload["collapsed_fields"] == nil {
		t.Fatalf("missing detail references: %#v", payload)
	}
}

func TestResultRecordIncludesFailure(t *testing.T) {
	record := ResultRecord("run_test", map[string]any{"status": "failed"}, errors.New("build failed token=secret-value"))
	if record.OK == nil || *record.OK || record.Error == nil || record.Error.Code != "run_failed" {
		t.Fatalf("record=%#v", record)
	}
	if record.Error.Message == "build failed token=secret-value" {
		t.Fatalf("error was not redacted: %#v", record.Error)
	}
	var out bytes.Buffer
	if err := WriteRecord(&out, record); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
}
