package run

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRawLineAlwaysReturnsJSON(t *testing.T) {
	for _, input := range []string{`{"type":"turn.started"}`, "plain stderr"} {
		got := normalizeRawLine(input)
		if !json.Valid([]byte(got)) {
			t.Fatalf("not JSON: %q", got)
		}
	}
}
