package streaming

import (
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestParseMode(t *testing.T) {
	for _, value := range []string{"pretty", "ui", "events", "jsonl", "raw", "off"} {
		if got, err := ParseMode(value); err != nil || string(got) != value {
			t.Fatalf("ParseMode(%q)=%q, %v", value, got, err)
		}
	}
	if _, err := ParseMode("verbose"); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestPrettyLineCollapsesActivityCompletion(t *testing.T) {
	started := map[string]bool{}
	begin := event("agent.activity", `{"message":"go test ./...","kind":"command","status":"in_progress","activity_id":"item_1"}`)
	if got := PrettyLine(&begin, started); !strings.Contains(got, "Run") || !strings.Contains(got, "go test ./...") {
		t.Fatalf("begin=%q", got)
	}
	end := event("agent.activity", `{"message":"go test ./...","kind":"command","status":"completed","activity_id":"item_1","exit_code":0,"duration_ms":1200}`)
	if got := PrettyLine(&end, started); got != "             passed | 1.2s" {
		t.Fatalf("end=%q", got)
	}
}

func TestPrettyLineDoesNotRenderActivityDetail(t *testing.T) {
	e := event("agent.activity", `{"message":"internal/app.go","kind":"file_change","status":"completed","detail":"entire file contents"}`)
	got := PrettyLine(&e, map[string]bool{})
	if strings.Contains(got, "entire file contents") || !strings.Contains(got, "Updated") {
		t.Fatalf("line=%q", got)
	}
}

func event(typ, payload string) state.Event {
	return state.Event{ID: "evt_test", Type: typ, PayloadJSON: payload}
}
