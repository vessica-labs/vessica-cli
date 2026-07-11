package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestActivityUpdatesMergeIntoOneExpandableRow(t *testing.T) {
	m := NewModel("test", func(*state.Event) string { return "full command output" })
	m.addEvent(state.Event{ID: "evt_1", Type: "agent.activity", PayloadJSON: `{"message":"pnpm test","kind":"command","status":"in_progress","activity_id":"item_1"}`})
	m.addEvent(state.Event{ID: "evt_2", Type: "agent.activity", PayloadJSON: `{"message":"pnpm test","kind":"command","status":"completed","activity_id":"item_1"}`})
	if len(m.rows) != 1 || m.rows[0].event.ID != "evt_2" {
		t.Fatalf("rows=%#v", m.rows)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.rows[0].expanded || m.rows[0].detail == "" {
		t.Fatalf("row did not expand: %#v", m.rows[0])
	}
}

func TestInitRequestsTerminalSize(t *testing.T) {
	m := NewModel("test", nil)
	if m.Init() == nil {
		t.Fatal("expected a terminal size command")
	}
}

func TestFollowsLatestEventAndFillsTerminalHeight(t *testing.T) {
	m := NewModel("test", nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	for i := range 40 {
		m.addEvent(messageEvent(i))
	}
	if !m.follow || m.cursor != 39 {
		t.Fatalf("follow=%v cursor=%d", m.follow, m.cursor)
	}
	view := m.View()
	if got := len(strings.Split(view, "\n")); got != 30 {
		t.Fatalf("view lines=%d, want 30\n%s", got, view)
	}
	if !strings.Contains(view, "message 39") || strings.Contains(view, "message 00") {
		t.Fatalf("viewport did not advance to latest events:\n%s", view)
	}
	shown := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "message ") {
			shown++
		}
	}
	if shown != 26 {
		t.Fatalf("shown rows=%d, want 26", shown)
	}
}

func TestMovingUpPausesFollowUntilReturningToBottom(t *testing.T) {
	m := NewModel("test", nil)
	for i := range 5 {
		m.addEvent(messageEvent(i))
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.follow || m.cursor != 3 {
		t.Fatalf("follow=%v cursor=%d", m.follow, m.cursor)
	}
	m.addEvent(messageEvent(5))
	if m.cursor != 3 {
		t.Fatalf("cursor advanced while follow was paused: %d", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if !m.follow || m.cursor != 5 {
		t.Fatalf("follow=%v cursor=%d", m.follow, m.cursor)
	}
}

func messageEvent(index int) state.Event {
	return state.Event{
		ID:          fmt.Sprintf("evt_%02d", index),
		Type:        "agent.message",
		PayloadJSON: fmt.Sprintf(`{"message":"message %02d","kind":"message"}`, index),
	}
}

func TestDoneKeepsUIOpenForInspection(t *testing.T) {
	m := NewModel("test", nil)
	updated, cmd := m.Update(DoneMsg{Status: "completed"})
	if cmd != nil || updated.(Model).doneStatus != "completed" {
		t.Fatal("done should update the footer without closing the UI")
	}
}
