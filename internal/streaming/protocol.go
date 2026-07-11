package streaming

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const ProtocolSchema = "vessica.stream/v1"

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ProtocolEvent struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	SandboxID string `json:"sandbox_id,omitempty"`
	Seq       int64  `json:"seq"`
	Type      string `json:"type"`
	Payload   any    `json:"payload"`
	CreatedAt string `json:"created_at"`
}

type ProtocolRecord struct {
	Schema    string         `json:"schema"`
	Kind      string         `json:"kind"`
	RunID     string         `json:"run_id,omitempty"`
	Seq       int64          `json:"seq,omitempty"`
	Timestamp string         `json:"timestamp"`
	Event     *ProtocolEvent `json:"event,omitempty"`
	OK        *bool          `json:"ok,omitempty"`
	Data      any            `json:"data,omitempty"`
	Error     *ProtocolError `json:"error,omitempty"`
}

func EventRecord(event *state.Event) ProtocolRecord {
	var payload any
	if event != nil {
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			payload = event.PayloadJSON
		}
		payload = compactPayload(payload)
		return ProtocolRecord{
			Schema:    ProtocolSchema,
			Kind:      "event",
			RunID:     event.RunID,
			Seq:       event.Seq,
			Timestamp: event.CreatedAt,
			Event: &ProtocolEvent{
				ID:        event.ID,
				RunID:     event.RunID,
				SandboxID: event.SandboxID,
				Seq:       event.Seq,
				Type:      event.Type,
				Payload:   payload,
				CreatedAt: event.CreatedAt,
			},
		}
	}
	return ProtocolRecord{Schema: ProtocolSchema, Kind: "event", Timestamp: timestamp()}
}

func compactPayload(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		var collapsed []string
		for key, child := range typed {
			if isHeavyField(key) {
				collapsed = append(collapsed, key)
				continue
			}
			out[key] = compactPayloadValue(key, child)
		}
		if len(collapsed) > 0 {
			sort.Strings(collapsed)
			out["collapsed_fields"] = collapsed
		}
		return out
	case []any:
		limit := len(typed)
		if limit > 100 {
			limit = 100
		}
		out := make([]any, 0, limit+1)
		for _, child := range typed[:limit] {
			out = append(out, compactPayload(child))
		}
		if len(typed) > limit {
			out = append(out, map[string]any{"truncated_items": len(typed) - limit})
		}
		return out
	default:
		return value
	}
}

func compactPayloadValue(key string, value any) any {
	if text, ok := value.(string); ok {
		limit := 4096
		if key == "message" {
			limit = 16 * 1024
		}
		if len(text) > limit {
			truncated := text[:limit]
			for !utf8.ValidString(truncated) {
				truncated = truncated[:len(truncated)-1]
			}
			return truncated + "... [truncated]"
		}
		return text
	}
	return compactPayload(value)
}

func isHeavyField(key string) bool {
	switch strings.ToLower(key) {
	case "detail", "command", "output", "aggregated_output", "body", "body_json", "content", "prompt", "system_prompt", "instructions", "artifact_context", "ticket_context":
		return true
	default:
		return false
	}
}

func ResultRecord(runID string, data any, runErr error) ProtocolRecord {
	ok := runErr == nil
	record := ProtocolRecord{
		Schema:    ProtocolSchema,
		Kind:      "result",
		RunID:     runID,
		Timestamp: timestamp(),
		OK:        &ok,
		Data:      data,
	}
	if runErr != nil {
		record.Error = &ProtocolError{Code: "run_failed", Message: redaction.Redact(runErr.Error())}
	}
	return record
}

func WriteRecord(w io.Writer, record ProtocolRecord) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(record)
}

func WriteEvent(w io.Writer, event *state.Event) error {
	return WriteRecord(w, EventRecord(event))
}

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
