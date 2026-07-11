package repo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitCommandContext runs trusted orchestration operations with the real Git
// binary. Coding agents still resolve `git` through PATH and remain subject to
// the sandbox's safe-git policy.
func GitCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, trustedGitBinary(), args...)
}

func trustedGitBinary() string {
	if configured := strings.TrimSpace(os.Getenv("VES_GIT_BINARY")); configured != "" {
		return configured
	}
	found, err := exec.LookPath("git")
	if err != nil || !isSafeGitWrapper(found) {
		return "git"
	}
	for _, candidate := range []string{"/usr/bin/git", "/usr/lib/safe-tools/git"} {
		if filepath.Clean(candidate) == filepath.Clean(found) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return found
}

func isSafeGitWrapper(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	buf := make([]byte, 1024)
	n, _ := file.Read(buf)
	return bytes.Contains(buf[:n], []byte("safe-git"))
}
