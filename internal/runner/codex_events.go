package runner

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type codexEventParser struct {
	started map[string]time.Time
}

func newCodexEventParser() *codexEventParser {
	return &codexEventParser{started: map[string]time.Time{}}
}

func (p *codexEventParser) parse(line, role string) Event {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Event{Type: "agent.warning", Message: line, Data: map[string]any{"kind": "unparsed", "role": role}, Raw: line}
	}
	typ, _ := raw["type"].(string)
	base := map[string]any{"role": role, "codex_type": typ}
	switch typ {
	case "thread.started":
		base["kind"] = "session"
		base["status"] = "started"
		base["thread_id"] = raw["thread_id"]
		return Event{Type: "agent.activity", Message: "Codex session started", Data: base, Raw: line}
	case "turn.started":
		base["kind"] = "turn"
		base["status"] = "started"
		return Event{Type: "agent.activity", Message: "Agent started", Data: base, Raw: line}
	case "turn.completed":
		base["kind"] = "usage"
		base["status"] = "completed"
		if usage, ok := raw["usage"].(map[string]any); ok {
			for k, v := range usage {
				base[k] = v
			}
		}
		return Event{Type: "agent.usage", Message: "Agent turn completed", Data: base, Raw: line}
	case "turn.failed":
		base["kind"] = "turn"
		base["status"] = "failed"
		base["detail"] = raw["error"]
		return Event{Type: "agent.error", Message: "Agent turn failed", Data: base, Raw: line}
	case "item.started", "item.updated", "item.completed":
		item, _ := raw["item"].(map[string]any)
		return p.parseItem(typ, item, base, line)
	default:
		base["kind"] = "codex_event"
		base["status"] = strings.TrimPrefix(typ, "item.")
		base["detail"] = line
		return Event{Type: "agent.activity", Message: typ, Data: base, Raw: line}
	}
}

func (p *codexEventParser) parseItem(eventType string, item, data map[string]any, rawLine string) Event {
	id, _ := item["id"].(string)
	kind, _ := item["type"].(string)
	status, _ := item["status"].(string)
	if status == "" {
		status = strings.TrimPrefix(eventType, "item.")
	}
	data["activity_id"] = id
	data["item_type"] = kind
	data["status"] = status
	if eventType == "item.started" && id != "" {
		p.started[id] = time.Now()
	}
	if started, ok := p.started[id]; ok && eventType == "item.completed" {
		data["duration_ms"] = time.Since(started).Milliseconds()
		delete(p.started, id)
	}

	switch kind {
	case "command_execution":
		command, _ := item["command"].(string)
		output, _ := item["aggregated_output"].(string)
		commandKind := classifyCommand(command)
		data["kind"] = commandKind
		data["command"] = command
		data["detail"] = output
		data["exit_code"] = item["exit_code"]
		message := commandSummary(command)
		if commandKind == "file_change" {
			message = fileChangeSummary(command)
		}
		return Event{Type: "agent.activity", Message: message, Data: data, Raw: rawLine}
	case "agent_message":
		text, _ := item["text"].(string)
		data["kind"] = "message"
		data["detail"] = text
		return Event{Type: "agent.message", Message: text, Data: data, Raw: rawLine}
	case "file_change", "file_changes":
		data["kind"] = "file_change"
		data["detail"] = item
		paths := changedPaths(item)
		data["files"] = paths
		message := "files"
		if len(paths) == 1 {
			message = paths[0]
		} else if len(paths) > 1 {
			message = fmt.Sprintf("%d files", len(paths))
		}
		return Event{Type: "agent.activity", Message: message, Data: data, Raw: rawLine}
	case "mcp_tool_call", "tool_call":
		name := firstString(item, "tool", "name", "server")
		data["kind"] = "tool"
		data["tool"] = name
		data["detail"] = item
		return Event{Type: "agent.activity", Message: name, Data: data, Raw: rawLine}
	case "web_search":
		query := firstString(item, "query", "text")
		data["kind"] = "search"
		data["detail"] = item
		return Event{Type: "agent.activity", Message: query, Data: data, Raw: rawLine}
	default:
		data["kind"] = kind
		data["detail"] = item
		message := strings.ReplaceAll(kind, "_", " ")
		if message == "" {
			message = "Agent activity"
		}
		return Event{Type: "agent.activity", Message: message, Data: data, Raw: rawLine}
	}
}

func classifyCommand(command string) string {
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "apply_patch") || strings.Contains(lower, "git apply"):
		return "file_change"
	case strings.Contains(lower, "rg ") || strings.Contains(lower, "grep ") || strings.Contains(lower, "find "):
		return "search"
	case strings.Contains(lower, "sed -n") || strings.Contains(lower, "head ") || strings.Contains(lower, "tail ") || strings.Contains(lower, "cat "):
		return "file_read"
	default:
		return "command"
	}
}

func commandSummary(command string) string {
	command = strings.TrimSpace(command)
	for _, prefix := range []string{"/bin/zsh -lc ", "/bin/bash -lc ", "bash -lc ", "sh -lc "} {
		command = strings.TrimPrefix(command, prefix)
	}
	command = strings.Trim(command, `"'`)
	if len(command) > 120 {
		command = command[:117] + "..."
	}
	return command
}

func fileChangeSummary(command string) string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(command, "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range []string{"*** Update File:", "*** Add File:", "*** Delete File:", "*** Move to:"} {
			if strings.HasPrefix(line, marker) {
				path := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, marker)))
				if path != "" && !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
			}
		}
	}
	if len(paths) == 1 {
		return paths[0]
	}
	if len(paths) > 1 {
		return fmt.Sprintf("%d files", len(paths))
	}
	return "files"
}

func changedPaths(item map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	if changes, ok := item["changes"].([]any); ok {
		for _, raw := range changes {
			if change, ok := raw.(map[string]any); ok {
				add(firstString(change, "path", "file"))
			}
		}
	}
	add(firstString(item, "path", "file"))
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
