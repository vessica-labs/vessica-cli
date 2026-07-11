package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

// Envelope is the machine-safe JSON response shape.
type Envelope struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is a structured CLI error.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Error is a structured CLI error. Printed is true when the user-facing
// representation has already been emitted.
type Error struct {
	Body    ErrorBody
	Printed bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Body.Code, e.Body.Message)
}

// IsPrinted reports whether err has already been emitted by a Printer.
func IsPrinted(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Printed
	}
	return false
}

// PrintError writes a single structured error envelope.
func PrintError(w io.Writer, code, message, hint string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(Envelope{OK: false, Error: &ErrorBody{Code: code, Message: message, Hint: hint}})
}

// Printer writes human or JSON output.
type Printer struct {
	JSON    bool
	Quiet   bool
	NoColor bool
	Out     io.Writer
	Err     io.Writer
}

func New(jsonMode, quiet, noColor bool) *Printer {
	return &Printer{
		JSON:    jsonMode,
		Quiet:   quiet,
		NoColor: noColor,
		Out:     os.Stdout,
		Err:     os.Stderr,
	}
}

func (p *Printer) Success(data any) error {
	if p.JSON {
		// Avoid null for empty slices in agent-facing JSON.
		if data == nil {
			data = map[string]any{}
		}
		return p.writeJSON(Envelope{OK: true, Data: data})
	}
	if p.Quiet {
		return nil
	}
	switch v := data.(type) {
	case string:
		_, err := fmt.Fprintln(p.Out, v)
		return err
	case fmt.Stringer:
		_, err := fmt.Fprintln(p.Out, v.String())
		return err
	case nil:
		return nil
	default:
		_, err := fmt.Fprintln(p.Out, FormatHuman(data))
		return err
	}
}

func (p *Printer) Fail(code, message, hint string) error {
	body := ErrorBody{Code: code, Message: message, Hint: hint}
	if p.JSON {
		_ = p.writeJSON(Envelope{OK: false, Error: &body})
		return &Error{Body: body, Printed: true}
	}
	msg := fmt.Sprintf("error: %s", message)
	if hint != "" {
		msg += "\n  hint: " + hint
	}
	_, _ = fmt.Fprintln(p.Err, msg)
	return &Error{Body: body, Printed: true}
}

func (p *Printer) Info(format string, args ...any) {
	if p.JSON || p.Quiet {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format+"\n", args...)
}

func (p *Printer) Debug(enabled bool, format string, args ...any) {
	if !enabled || p.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.Err, "debug: "+format+"\n", args...)
}

func (p *Printer) writeJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// PrintJSONL writes a single JSONL object (for event streams).
func PrintJSONL(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

func FormatHuman(data any) string {
	v := normalize(data)
	return strings.TrimRight(formatValue(v, 0), "\n")
}

func normalize(data any) any {
	if data == nil {
		return nil
	}
	if s, ok := data.(fmt.Stringer); ok {
		return s.String()
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprint(data)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Sprint(data)
	}
	return out
}

func formatValue(v any, indent int) string {
	prefix := strings.Repeat(" ", indent)
	switch x := v.(type) {
	case nil:
		return prefix + "(none)\n"
	case string:
		if strings.TrimSpace(x) == "" {
			return prefix + "(empty)\n"
		}
		return prefix + truncateScalar(x) + "\n"
	case bool, float64:
		return prefix + fmt.Sprint(x) + "\n"
	case []any:
		if len(x) == 0 {
			return prefix + "(none)\n"
		}
		var b strings.Builder
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				b.WriteString(formatMapItem(m, indent))
			} else {
				b.WriteString(prefix + "- " + strings.TrimSpace(formatValue(item, 0)) + "\n")
			}
		}
		return b.String()
	case map[string]any:
		return formatMap(x, indent)
	default:
		rv := reflect.ValueOf(v)
		if rv.IsValid() {
			return prefix + fmt.Sprint(v) + "\n"
		}
		return prefix + "(none)\n"
	}
}

func formatMap(m map[string]any, indent int) string {
	prefix := strings.Repeat(" ", indent)
	keys := sortedNonEmptyKeys(m, "")
	if len(keys) == 0 {
		return prefix + "(none)\n"
	}
	var b strings.Builder
	for _, k := range keys {
		v := m[k]
		if isScalar(v) {
			b.WriteString(prefix + humanKey(k) + ": " + truncateScalar(fmt.Sprint(v)) + "\n")
			continue
		}
		b.WriteString(prefix + humanKey(k) + ":\n")
		b.WriteString(formatValue(v, indent+2))
	}
	return b.String()
}

func formatMapItem(m map[string]any, indent int) string {
	prefix := strings.Repeat(" ", indent)
	var head []string
	if id := stringField(m, "id"); id != "" {
		head = append(head, id)
	}
	if title := firstStringField(m, "title", "name", "summary", "phase", "type"); title != "" {
		head = append(head, title)
	}
	if status := stringField(m, "status"); status != "" {
		head = append(head, "["+status+"]")
	}
	if len(head) == 0 {
		head = append(head, "item")
	}
	var b strings.Builder
	b.WriteString(prefix + "- " + strings.Join(head, " ") + "\n")
	for _, k := range sortedNonEmptyKeys(m, "id", "title", "name", "summary", "status") {
		v := m[k]
		if isScalar(v) {
			b.WriteString(prefix + "  " + humanKey(k) + ": " + truncateScalar(fmt.Sprint(v)) + "\n")
			continue
		}
		b.WriteString(prefix + "  " + humanKey(k) + ":\n")
		b.WriteString(formatValue(v, indent+4))
	}
	return b.String()
}

func sortedNonEmptyKeys(m map[string]any, omit ...string) []string {
	omitSet := map[string]bool{}
	for _, k := range omit {
		omitSet[k] = true
	}
	var keys []string
	for k, v := range m {
		if omitSet[k] || emptyValue(v) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringField(m, k); s != "" {
			return s
		}
	}
	return ""
}

func stringField(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, bool, float64:
		return true
	default:
		return false
	}
}

func emptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}

func humanKey(k string) string {
	return strings.ReplaceAll(k, "_", " ")
}

func truncateScalar(s string) string {
	s = strings.TrimSpace(s)
	const max = 500
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
