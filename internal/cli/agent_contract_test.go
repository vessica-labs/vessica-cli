package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/output"
)

func TestCapabilitiesAndEpicSpecAgentContract(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--json")
	raw := runCLI(t, dir, "capabilities", "--json")
	var caps struct {
		Schema string `json:"schema"`
		OK     bool   `json:"ok"`
		Data   struct {
			Workspace struct {
				Initialized bool `json:"initialized"`
			} `json:"workspace"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Schema != output.EnvelopeSchema || !caps.OK || !caps.Data.Workspace.Initialized {
		t.Fatalf("capabilities=%s", raw)
	}
	specPath := filepath.Join(dir, "epic.json")
	if err := os.WriteFile(specPath, []byte(`{"title":"Agent API","tickets":[{"key":"api","title":"Build API"},{"key":"plugin","title":"Build plugin","depends_on":["api"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	draft := runCLI(t, dir, "epic", "draft", "--spec-file", specPath, "--json")
	var drafted struct {
		Data struct {
			Valid bool `json:"valid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(draft), &drafted); err != nil || !drafted.Data.Valid {
		t.Fatalf("draft=%s err=%v", draft, err)
	}
	created := runCLI(t, dir, "epic", "add", "--spec-file", specPath, "--yes", "--idempotency-key", "epic-agent-contract", "--json")
	var result struct {
		Data struct {
			Tickets []any `json:"tickets"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(created), &result); err != nil || len(result.Data.Tickets) != 2 {
		t.Fatalf("created=%s err=%v", created, err)
	}
}
