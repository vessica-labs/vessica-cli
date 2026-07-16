package cli

import (
	"encoding/json"
	"testing"
)

func TestSandboxPromptDryRunContract(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	raw := runCLI(t, dir, "sandbox", "prompt", "sbx_test", "Tighten the heading", "--no-push", "--stream", "events", "--dry-run", "--json")
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			DryRun bool   `json:"dry_run"`
			Action string `json:"action"`
			Would  struct {
				SandboxID string `json:"sandbox_id"`
				Prompt    string `json:"prompt"`
				Push      bool   `json:"push"`
			} `json:"would"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, raw)
	}
	if !envelope.OK || !envelope.Data.DryRun || envelope.Data.Action != "sandbox.prompt" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	if envelope.Data.Would.SandboxID != "sbx_test" || envelope.Data.Would.Prompt != "Tighten the heading" || envelope.Data.Would.Push {
		t.Fatalf("unexpected dry run: %#v", envelope.Data.Would)
	}
}

func TestRunApproveDryRunContract(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	raw := runCLI(t, dir, "run", "approve", "run_test", "--merge-method", "rebase", "--keep-preview", "--dry-run", "--json")
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Action string `json:"action"`
			Would  struct {
				RunID       string `json:"run_id"`
				MergeMethod string `json:"merge_method"`
				KeepPreview bool   `json:"keep_preview"`
			} `json:"would"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, raw)
	}
	if !envelope.OK || envelope.Data.Action != "run.approve" || envelope.Data.Would.RunID != "run_test" || envelope.Data.Would.MergeMethod != "rebase" || !envelope.Data.Would.KeepPreview {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}
