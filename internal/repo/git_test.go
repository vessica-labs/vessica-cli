package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafeGitWrapper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n# safe-git policy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isSafeGitWrapper(path) {
		t.Fatal("expected safe-git wrapper to be detected")
	}
}

func TestGitCommandDisablesRepositoryHooks(t *testing.T) {
	t.Setenv("VES_GIT_BINARY", "/usr/bin/git")
	cmd := GitCommandContext(context.Background(), "status")
	if len(cmd.Args) < 4 || cmd.Args[1] != "-c" || cmd.Args[2] != "core.hooksPath=/dev/null" {
		t.Fatalf("git args do not disable repository hooks: %v", cmd.Args)
	}
}

func TestTrustedGitBinaryHonorsOverride(t *testing.T) {
	t.Setenv("VES_GIT_BINARY", "/custom/git")
	if got := trustedGitBinary(); got != "/custom/git" {
		t.Fatalf("got %q", got)
	}
}
