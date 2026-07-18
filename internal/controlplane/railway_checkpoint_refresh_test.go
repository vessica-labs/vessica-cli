package controlplane

import (
	"strings"
	"testing"
)

func TestRepositoryCheckpointScrubUsesTrustedGit(t *testing.T) {
	script := repositoryCheckpointScrubScript("encoded")
	if strings.Contains(script, "\ngit -C") || !strings.Contains(script, "trusted_git=/usr/bin/git") {
		t.Fatalf("checkpoint scrub must bypass the agent-facing safe-git wrapper:\n%s", script)
	}
	if !strings.Contains(script, "printf '%s' 'encoded' | base64 -d") {
		t.Fatalf("checkpoint marker payload missing from scrub script:\n%s", script)
	}
	if !strings.Contains(script, "rm -f /workspace/repo/.vessica/config.yaml") {
		t.Fatalf("run-local repository attachment was not scrubbed:\n%s", script)
	}
}
