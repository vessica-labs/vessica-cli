package streaming

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

type Mode string

const (
	ModePretty Mode = "pretty"
	ModeUI     Mode = "ui"
	ModeEvents Mode = "events"
	ModeJSONL  Mode = "jsonl"
	ModeRaw    Mode = "raw"
	ModeOff    Mode = "off"
)

func ParseMode(raw string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ModePretty:
		return ModePretty, nil
	case ModeUI, ModeEvents, ModeJSONL, ModeRaw, ModeOff:
		return Mode(strings.ToLower(strings.TrimSpace(raw))), nil
	default:
		return "", fmt.Errorf("invalid stream mode %q (use pretty, ui, events, jsonl, raw, or off)", raw)
	}
}

type Renderer struct {
	mu          sync.Mutex
	Out         io.Writer
	Err         io.Writer
	currentRole string
	started     map[string]bool
}

func NewRenderer(out, errOut io.Writer) *Renderer {
	return &Renderer{Out: out, Err: errOut, started: map[string]bool{}}
}

func (r *Renderer) Render(e *state.Event) {
	if e == nil {
		return
	}
	payload := Payload(e)
	r.mu.Lock()
	defer r.mu.Unlock()

	role := stringValue(payload["role"])
	if role != "" && role != r.currentRole && strings.HasPrefix(e.Type, "agent.") {
		if r.currentRole != "" {
			_, _ = fmt.Fprintln(r.Out)
		}
		_, _ = fmt.Fprintln(r.Out, roleTitle(role))
		r.currentRole = role
	}

	if line := PrettyLine(e, r.started); line != "" {
		_, _ = fmt.Fprintln(r.Out, line)
	}
}

func PrettyLine(e *state.Event, started map[string]bool) string {
	payload := Payload(e)
	message := stringValue(payload["message"])
	switch e.Type {
	case "agent.activity":
		kind := stringValue(payload["kind"])
		status := stringValue(payload["status"])
		id := stringValue(payload["activity_id"])
		if kind == "session" || kind == "turn" || kind == "usage" || kind == "codex_event" {
			return ""
		}
		if status == "in_progress" || status == "started" {
			if id != "" {
				started[id] = true
			}
			return fmt.Sprintf("  %-9s %s", activityVerb(kind, false), message)
		}
		result := statusLabel(status, payload["exit_code"])
		duration := durationLabel(payload["duration_ms"])
		if id != "" && started[id] {
			delete(started, id)
			if duration != "" {
				result += " | " + duration
			}
			return "             " + result
		}
		line := fmt.Sprintf("  %-9s %s", activityVerb(kind, true), message)
		if result != "" {
			line += "  " + result
		}
		return line
	case "agent.message":
		if strings.TrimSpace(message) == "" || message == "codex completed" {
			return ""
		}
		return indentMultiline(message, "  ")
	case "agent.usage":
		input := numberLabel(payload["input_tokens"])
		output := numberLabel(payload["output_tokens"])
		if input == "" && output == "" {
			return ""
		}
		return fmt.Sprintf("  Tokens    %s in | %s out", input, output)
	case "agent.error", "error":
		return "  Error     " + message
	case "agent.warning", "warning":
		if !strings.Contains(strings.ToLower(message), "error") && !strings.Contains(strings.ToLower(message), "failed") {
			return ""
		}
		return "  Warning   " + oneLine(message, 180)
	case "run.phase.started":
		return "\n" + strings.ToUpper(stringValue(payload["phase"]))
	case "run.phase.completed":
		return "  Completed " + stringValue(payload["phase"])
	case "ticket.claimed":
		return "  Ticket    " + stringValue(payload["ticket_id"])
	case "merge.success", "repo.pr.created", "preview.ready", "sandbox.ready":
		if message != "" {
			return "  " + message
		}
		return "  " + strings.ReplaceAll(e.Type, ".", " ")
	case "run.completed":
		return "\nRun " + stringValue(payload["status"])
	default:
		return ""
	}
}

func EventSummary(e *state.Event) string {
	payload := Payload(e)
	message := stringValue(payload["message"])
	if e.Type == "agent.activity" {
		kind := stringValue(payload["kind"])
		status := statusLabel(stringValue(payload["status"]), payload["exit_code"])
		line := strings.TrimSpace(fmt.Sprintf("%s %s", activityVerb(kind, true), message))
		if status != "" {
			line += "  " + status
		}
		return line
	}
	if message != "" {
		return oneLine(message, 180)
	}
	return strings.ReplaceAll(e.Type, ".", " ")
}

func EventDetail(e *state.Event) string {
	payload := Payload(e)
	if detail, ok := payload["detail"]; ok && detail != nil {
		switch value := detail.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return value
			}
		default:
			b, _ := json.MarshalIndent(value, "", "  ")
			return string(b)
		}
	}
	if command := stringValue(payload["command"]); command != "" {
		return command
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return string(b)
}

func Payload(e *state.Event) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
	if payload == nil {
		payload = map[string]any{}
	}
	return payload
}

func activityVerb(kind string, completed bool) string {
	switch kind {
	case "file_read":
		return "Read"
	case "file_change":
		return "Updated"
	case "search":
		if completed {
			return "Searched"
		}
		return "Search"
	case "tool":
		if completed {
			return "Called"
		}
		return "Call"
	case "command":
		if completed {
			return "Ran"
		}
		return "Run"
	default:
		label := strings.ReplaceAll(kind, "_", " ")
		if label == "" {
			return "Activity"
		}
		return strings.ToUpper(label[:1]) + label[1:]
	}
}

func statusLabel(status string, exitCode any) string {
	if code, ok := exitCode.(float64); ok {
		if code == 0 {
			return "passed"
		}
		return fmt.Sprintf("failed (%d)", int(code))
	}
	switch status {
	case "completed", "ok", "passed":
		return "passed"
	case "failed", "error":
		return "failed"
	case "in_progress", "started":
		return "running"
	default:
		return status
	}
}

func durationLabel(value any) string {
	ms, ok := value.(float64)
	if !ok || ms <= 0 {
		return ""
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", int(ms))
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

func numberLabel(value any) string {
	n, ok := value.(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.0f", n)
}

func roleTitle(role string) string {
	if role == "" {
		return "Agent"
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

func indentMultiline(value, prefix string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func oneLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > max {
		return value[:max-3] + "..."
	}
	return value
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}
