// Package isolation defines the hosted coding-agent process boundary.
package isolation

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Environment returns the allowlisted environment for untrusted repository
// processes. Explicit values are trusted orchestration inputs.
func Environment(extra map[string]string) []string {
	return environment(extra, false)
}

// TrustGitWorkdir records one exact worktree as safe for the isolated runner.
// Hosted worktrees intentionally keep their Git metadata privileged, so Git's
// ownership check must be satisfied without granting a wildcard exception.
func TrustGitWorkdir(ctx context.Context, workdir string) error {
	name := strings.TrimSpace(os.Getenv("VES_RUNNER_USER"))
	workdir = strings.TrimSpace(workdir)
	if name == "" || workdir == "" {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(workdir, ".git")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect Git worktree metadata: %w", err)
	}
	exact := exactSafeDirectoryRegex(workdir)
	configure := CommandContext(ctx, workdir, "git", "config", "--global", "--replace-all", "safe.directory", workdir, exact)
	if output, err := configure.CombinedOutput(); err != nil {
		return fmt.Errorf("trust isolated Git worktree %s: %w: %s", workdir, err, strings.TrimSpace(string(output)))
	}
	verify := CommandContext(ctx, workdir, "git", "-C", workdir, "status", "--porcelain", "--untracked-files=no")
	if output, err := verify.CombinedOutput(); err != nil {
		return fmt.Errorf("verify isolated Git worktree %s: %w: %s", workdir, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func exactSafeDirectoryRegex(workdir string) string {
	return "^" + regexp.QuoteMeta(workdir) + "$"
}

// ModelEnvironment adds only the model credential and external-sandbox marker
// needed by the Codex process itself. Repository build commands never receive it.
func ModelEnvironment(extra map[string]string) []string {
	return environment(extra, true)
}

func environment(extra map[string]string, model bool) []string {
	allowed := []string{
		"PATH", "LANG", "LC_ALL", "TERM", "TMPDIR", "NODE_PATH", "PLAYWRIGHT_BROWSERS_PATH",
	}
	if model {
		allowed = append(allowed, "OPENAI_API_KEY", "VES_CODEX_EXTERNAL_SANDBOX")
	}
	values := make(map[string]string, len(allowed)+len(extra)+1)
	for _, key := range allowed {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	home := strings.TrimSpace(os.Getenv("VES_RUNNER_HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		values["HOME"] = home
	}
	for key, value := range extra {
		values[key] = value
	}
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

// PrepareWorkdir grants the isolated user ownership of working-tree content
// while preserving privileged ownership of .git metadata. The sticky worktree
// root lets the agent create files without replacing the protected .git entry.
func PrepareWorkdir(ctx context.Context, workdir string) error {
	name := strings.TrimSpace(os.Getenv("VES_RUNNER_USER"))
	if name == "" || strings.TrimSpace(workdir) == "" {
		return nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("lookup isolated runner user %s: %w", name, err)
	}
	if _, err := strconv.Atoi(account.Uid); err != nil {
		return fmt.Errorf("parse isolated runner uid: %w", err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return fmt.Errorf("parse isolated runner gid: %w", err)
	}
	find := exec.CommandContext(ctx, "find", workdir, "-mindepth", "1", "-name", ".git", "-prune", "-o", "-exec", "chown", "-h", account.Uid+":"+account.Gid, "--", "{}", "+")
	if output, err := find.CombinedOutput(); err != nil {
		return fmt.Errorf("grant isolated runner access to %s: %w: %s", workdir, err, strings.TrimSpace(string(output)))
	}
	if err := os.Chown(workdir, os.Getuid(), gid); err != nil {
		return fmt.Errorf("protect isolated worktree root: %w", err)
	}
	if err := os.Chmod(workdir, 0o1770); err != nil {
		return fmt.Errorf("set isolated worktree permissions: %w", err)
	}
	if err := protectGitMetadata(ctx, workdir); err != nil {
		return err
	}
	return nil
}

func protectGitMetadata(ctx context.Context, workdir string) error {
	entry := filepath.Join(workdir, ".git")
	info, err := os.Lstat(entry)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect git metadata: %w", err)
	}
	if err := os.Lchown(entry, os.Getuid(), os.Getgid()); err != nil {
		return fmt.Errorf("protect git metadata entry: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	owner := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	chown := exec.CommandContext(ctx, "find", entry, "-exec", "chown", "-h", owner, "--", "{}", "+")
	if output, err := chown.CombinedOutput(); err != nil {
		return fmt.Errorf("protect git metadata ownership: %w: %s", err, strings.TrimSpace(string(output)))
	}
	permissions := exec.CommandContext(ctx, "find", entry, "(", "-type", "d", "-o", "-type", "f", ")", "-exec", "chmod", "go-w", "--", "{}", "+")
	if output, err := permissions.CombinedOutput(); err != nil {
		return fmt.Errorf("protect git metadata permissions: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// CommandContext creates an untrusted repository command. Hosted workers run
// it as VES_RUNNER_USER with the allowlisted environment; local mode preserves
// the caller's normal process environment.
func CommandContext(ctx context.Context, workdir, command string, args ...string) *exec.Cmd {
	name := strings.TrimSpace(os.Getenv("VES_RUNNER_USER"))
	if name == "" {
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = workdir
		return cmd
	}
	wrapped := append([]string{"--user", name, "--preserve-environment", "--", command}, args...)
	cmd := exec.CommandContext(ctx, "runuser", wrapped...)
	cmd.Dir = workdir
	cmd.Env = Environment(nil)
	return cmd
}
