package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/isolation"
)

// DockerSandbox implements Sandbox with local Docker.
type DockerSandbox struct {
	sandboxID   string
	containerID string
	opts        CreateOpts
	status      string
	previewURL  string
	previewCmd  *exec.Cmd
}

func NewDocker(sandboxID string) *DockerSandbox {
	return &DockerSandbox{sandboxID: sandboxID, status: "pending"}
}

func (d *DockerSandbox) ID() string          { return d.sandboxID }
func (d *DockerSandbox) ContainerID() string { return d.containerID }
func (d *DockerSandbox) Workdir() string     { return d.opts.HostWorkdir }
func (d *DockerSandbox) SetContainerID(containerID, hostWorkdir string, previewPort int) {
	d.containerID = containerID
	d.status = "running"
	d.opts.HostWorkdir = hostWorkdir
	d.opts.PreviewPort = previewPort
}

func (d *DockerSandbox) Create(ctx context.Context, opts CreateOpts) error {
	d.opts = opts
	if opts.Image == "" {
		opts.Image = FallbackImage()
		d.opts.Image = opts.Image
	}
	name := "ves-" + d.sandboxID
	expiresAt := opts.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(24 * time.Hour)
		d.opts.ExpiresAt = expiresAt
	}
	// Remove stale container if any
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()

	args := []string{
		"create",
		"--rm",
		"--name", name,
		"-w", "/workspace",
		"-e", "VES_SANDBOX_ID=" + d.sandboxID,
		"-e", fmt.Sprintf("VES_SANDBOX_EXPIRES_EPOCH=%d", expiresAt.Unix()),
		"--label", "vessica.managed=true",
		"--label", "vessica.sandbox_id=" + d.sandboxID,
		"--label", "vessica.run_id=" + opts.RunID,
		"--label", "vessica.workspace_id=" + opts.WorkspaceID,
		"--label", "vessica.initial_expires_at=" + expiresAt.Format(time.RFC3339),
	}
	for k, v := range opts.Env {
		args = append(args, "-e", k+"="+v)
	}
	if opts.HostWorkdir != "" {
		args = append(args, "-v", opts.HostWorkdir+":/workspace")
	}
	if opts.PreviewPort > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", opts.PreviewPort))
	}
	args = append(args, opts.Image, "sh", "-lc", expiryWatchdogCommand())

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker create: %w: %s", err, strings.TrimSpace(string(out)))
	}
	d.containerID = strings.TrimSpace(string(out))
	d.status = "created"
	return nil
}

func (d *DockerSandbox) Start(ctx context.Context) error {
	if d.containerID == "" {
		return ErrNotRunning
	}
	out, err := exec.CommandContext(ctx, "docker", "start", d.containerID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start: %w: %s", err, strings.TrimSpace(string(out)))
	}
	d.status = "running"

	// Bootstrap: clone remote if provided and workspace empty
	if d.opts.RemoteURL != "" && d.opts.HostWorkdir == "" {
		remote := d.opts.RemoteURL
		if d.opts.Token != "" && strings.Contains(remote, "github.com") {
			remote = injectGitHubToken(remote, d.opts.Token)
		}
		clone := fmt.Sprintf("rm -rf /workspace/* /workspace/.[!.]* 2>/dev/null; git clone --depth 1 %s /workspace && cd /workspace", shellQuote(remote))
		if d.opts.Branch != "" {
			clone += fmt.Sprintf(" && git checkout -B %s", shellQuote(d.opts.Branch))
		}
		if _, err := d.Exec(ctx, []string{"bash", "-lc", clone}, os.Stdout, os.Stderr); err != nil {
			// Non-fatal if clone fails in offline/dev; mark status
			d.status = "running_no_clone"
		}
	} else if d.opts.Branch != "" && d.opts.HostWorkdir != "" {
		_, _ = d.Exec(ctx, []string{"bash", "-lc", "cd /workspace && git checkout -B " + shellQuote(d.opts.Branch)}, io.Discard, io.Discard)
	}
	return nil
}

func (d *DockerSandbox) Exec(ctx context.Context, cmd []string, stdout, stderr io.Writer) (int, error) {
	if d.containerID == "" {
		return 1, ErrNotRunning
	}
	args := append([]string{"exec", d.containerID}, cmd...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdout = stdout
	c.Stderr = stderr
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), err
	}
	return 1, err
}

func (d *DockerSandbox) Stream(ctx context.Context, stdout, stderr io.Writer) error {
	if d.containerID == "" {
		return ErrNotRunning
	}
	c := exec.CommandContext(ctx, "docker", "logs", "-f", d.containerID)
	c.Stdout = stdout
	c.Stderr = stderr
	return c.Run()
}

func (d *DockerSandbox) ExposePort(ctx context.Context, port int) (string, error) {
	if d.containerID == "" {
		return "", ErrNotRunning
	}
	// Publish via docker port mapping by recreating is heavy; use host network-style proxy with docker run -p on a sidecar.
	// For v1: start a simple socat/publish by docker run --link is deprecated; use `docker inspect` + host port if already published.
	out, err := exec.CommandContext(ctx, "docker", "port", d.containerID, fmt.Sprintf("%d", port)).CombinedOutput()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		parts := strings.Split(strings.TrimSpace(string(out)), ":")
		hostPort := parts[len(parts)-1]
		url := fmt.Sprintf("http://127.0.0.1:%s", hostPort)
		return url, nil
	}
	// Fallback local URL assumption when using host workdir + process on host
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

func (d *DockerSandbox) StartPreview(ctx context.Context, command string, port int, healthcheck string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("preview command is empty")
	}
	previewScript := "cd /workspace && nohup bash -lc " + shellQuote(command) + " > /tmp/ves-preview.log 2>&1 & echo $! > /tmp/ves-preview.pid"
	if _, err := d.Exec(ctx, []string{"bash", "-lc", previewScript}, io.Discard, io.Discard); err != nil {
		return "", err
	}
	url, err := d.ExposePort(ctx, port)
	if err != nil {
		return "", err
	}
	if healthcheck == "" {
		healthcheck = url
	} else {
		healthcheck = rewriteHealthcheckURL(healthcheck, url)
	}
	if err := waitForHTTP(ctx, healthcheck, 30*time.Second); err != nil {
		return "", err
	}
	d.previewURL = url
	return url, nil
}

func (d *DockerSandbox) StopPreview(ctx context.Context) error {
	if d.containerID == "" {
		return nil
	}
	stopScript := "if [ -f /tmp/ves-preview.pid ]; then kill $(cat /tmp/ves-preview.pid) 2>/dev/null || true; rm -f /tmp/ves-preview.pid; fi"
	_, _ = d.Exec(ctx, []string{"bash", "-lc", stopScript}, io.Discard, io.Discard)
	d.previewURL = ""
	return nil
}

func (d *DockerSandbox) RefreshLease(ctx context.Context, expiresAt time.Time) error {
	if d.containerID == "" {
		return ErrNotRunning
	}
	cmd := fmt.Sprintf("echo %d > /tmp/ves-expires-at", expiresAt.UTC().Unix())
	_, err := d.Exec(ctx, []string{"sh", "-lc", cmd}, io.Discard, io.Discard)
	return err
}

func (d *DockerSandbox) PreviewURL() string { return d.previewURL }

func (d *DockerSandbox) Destroy(ctx context.Context) error {
	name := d.containerID
	if name == "" {
		name = "ves-" + d.sandboxID
	}
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
	d.status = "destroyed"
	d.containerID = ""
	return nil
}

func (d *DockerSandbox) Status(ctx context.Context) (string, error) {
	if d.containerID == "" {
		return d.status, nil
	}
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", d.containerID).CombinedOutput()
	if err != nil {
		return d.status, nil
	}
	d.status = strings.TrimSpace(string(out))
	return d.status, nil
}

func expiryWatchdogCommand() string {
	return `echo "${VES_SANDBOX_EXPIRES_EPOCH:-0}" > /tmp/ves-expires-at
trap 'exit 0' TERM INT
while :; do
  now=$(date +%s)
  expires=$(cat /tmp/ves-expires-at 2>/dev/null || echo 0)
  if [ "$expires" -gt 0 ] && [ "$now" -ge "$expires" ]; then
    exit 0
  fi
  sleep 5
done`
}

// EnsureImage pulls image if missing.
func EnsureImage(ctx context.Context, image string) error {
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", image).Run(); err == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull %s: %w: %s", image, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func injectGitHubToken(remote, token string) string {
	remote = strings.TrimSpace(remote)
	if strings.HasPrefix(remote, "git@github.com:") {
		path := strings.TrimPrefix(remote, "git@github.com:")
		path = strings.TrimSuffix(path, ".git") + ".git"
		return fmt.Sprintf("https://x-access-token:%s@github.com/%s", token, path)
	}
	if strings.HasPrefix(remote, "https://github.com/") {
		rest := strings.TrimPrefix(remote, "https://github.com/")
		return fmt.Sprintf("https://x-access-token:%s@github.com/%s", token, rest)
	}
	return remote
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// LocalDevSandbox runs commands on the host workdir (used when Docker unavailable or --local).
type LocalDevSandbox struct {
	sandboxID      string
	workdir        string
	status         string
	previewURL     string
	previewCmd     *exec.Cmd
	previewLog     *os.File
	previewLogPath string
}

func NewLocalDev(sandboxID, workdir string) *LocalDevSandbox {
	return &LocalDevSandbox{sandboxID: sandboxID, workdir: workdir, status: "pending"}
}

func (l *LocalDevSandbox) ID() string          { return l.sandboxID }
func (l *LocalDevSandbox) ContainerID() string { return "local" }
func (l *LocalDevSandbox) Workdir() string     { return l.workdir }

func (l *LocalDevSandbox) Create(ctx context.Context, opts CreateOpts) error {
	if opts.HostWorkdir != "" {
		l.workdir = opts.HostWorkdir
	}
	if l.workdir == "" {
		return fmt.Errorf("local sandbox requires host workdir")
	}
	l.status = "created"
	return nil
}

func (l *LocalDevSandbox) Start(ctx context.Context) error {
	l.status = "running"
	return nil
}

func (l *LocalDevSandbox) Exec(ctx context.Context, cmd []string, stdout, stderr io.Writer) (int, error) {
	if len(cmd) == 0 {
		return 1, fmt.Errorf("empty command")
	}
	if err := isolation.PrepareWorkdir(ctx, l.workdir); err != nil {
		return 1, err
	}
	c := isolation.CommandContext(ctx, l.workdir, cmd[0], cmd[1:]...)
	c.Stdout = stdout
	c.Stderr = stderr
	err := c.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), err
	}
	return 1, err
}

func (l *LocalDevSandbox) Stream(ctx context.Context, stdout, stderr io.Writer) error {
	return nil
}

func (l *LocalDevSandbox) ExposePort(ctx context.Context, port int) (string, error) {
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

func (l *LocalDevSandbox) StartPreview(ctx context.Context, command string, port int, healthcheck string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("preview command is empty")
	}
	if err := isolation.PrepareWorkdir(ctx, l.workdir); err != nil {
		return "", err
	}
	if os.Getenv("VES_CODEX_EXTERNAL_SANDBOX") == "1" {
		logPath := filepath.Join(l.workdir, ".vessica-preview.log")
		pidPath := filepath.Join(l.workdir, ".vessica-preview.pid")
		script := "if test -f " + shellQuote(pidPath) + "; then preview_pgid=$(cat " + shellQuote(pidPath) + "); " +
			"if kill -0 -- -$preview_pgid 2>/dev/null; then exit 0; fi; fi; " +
			// Older workers killed only the preview wrapper and could leave Vinext
			// children occupying the configured port. This cleanup is deliberately
			// user-scoped and process-specific so a retained sandbox can recover.
			"pkill -TERM -u $(id -u) -f '[v]inext.* dev' 2>/dev/null || true; sleep 1; " +
			"pkill -KILL -u $(id -u) -f '[v]inext.* dev' 2>/dev/null || true; " +
			"nohup setsid bash -lc " + shellQuote(command) + " >>" + shellQuote(logPath) + " 2>&1 </dev/null & echo $! >" + shellQuote(pidPath)
		if output, err := isolation.CommandContext(ctx, l.workdir, "bash", "-lc", script).CombinedOutput(); err != nil {
			return "", fmt.Errorf("start detached preview: %w: %s", err, strings.TrimSpace(string(output)))
		}
		l.previewLogPath = logPath
	} else {
		cmd := isolation.CommandContext(context.Background(), l.workdir, "bash", "-lc", command)
		logFile, err := os.CreateTemp("", "ves-preview-*.log")
		if err == nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
			l.previewLog = logFile
			l.previewLogPath = logFile.Name()
		}
		if err := cmd.Start(); err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			return "", err
		}
		l.previewCmd = cmd
	}
	url, _ := l.ExposePort(ctx, port)
	if healthcheck == "" {
		healthcheck = url
	} else {
		healthcheck = rewriteHealthcheckURL(healthcheck, url)
	}
	if err := waitForHTTP(ctx, healthcheck, previewStartupTimeout()); err != nil {
		_ = l.StopPreview(context.Background())
		return "", err
	}
	l.previewURL = url
	return url, nil
}

func (l *LocalDevSandbox) StopPreview(ctx context.Context) error {
	if os.Getenv("VES_CODEX_EXTERNAL_SANDBOX") == "1" {
		pidPath := filepath.Join(l.workdir, ".vessica-preview.pid")
		stop := "if test -f " + shellQuote(pidPath) + "; then preview_pgid=$(cat " + shellQuote(pidPath) + "); " +
			"kill -TERM -- -$preview_pgid 2>/dev/null || true; sleep 1; kill -KILL -- -$preview_pgid 2>/dev/null || true; fi; " +
			"rm -f " + shellQuote(pidPath)
		_ = isolation.CommandContext(ctx, l.workdir, "bash", "-lc", stop).Run()
	}
	if l.previewCmd != nil && l.previewCmd.Process != nil {
		_ = l.previewCmd.Process.Kill()
		_, _ = l.previewCmd.Process.Wait()
	}
	if l.previewLog != nil {
		_ = l.previewLog.Close()
		l.previewLog = nil
	}
	// Hosted Railway workers keep the preview log in the retained workspace so
	// `ves sandbox logs` can explain startup failures after the worker exits.
	// The PR phase removes this runtime-only file before source changes are
	// committed, so retaining it here cannot leak into the proposed change.
	if l.previewLogPath != "" && os.Getenv("VES_CODEX_EXTERNAL_SANDBOX") != "1" {
		_ = os.Remove(l.previewLogPath)
	}
	l.previewLogPath = ""
	l.previewURL = ""
	return nil
}

func (l *LocalDevSandbox) RefreshLease(ctx context.Context, expiresAt time.Time) error {
	return nil
}

func (l *LocalDevSandbox) PreviewURL() string { return l.previewURL }

func (l *LocalDevSandbox) Destroy(ctx context.Context) error {
	l.status = "destroyed"
	return nil
}

func (l *LocalDevSandbox) Status(ctx context.Context) (string, error) {
	return l.status, nil
}

// WorktreePath returns a run-specific worktree under .vessica/sandboxes.
func WorktreePath(root, sandboxID string) string {
	return filepath.Join(root, ".vessica", "sandboxes", sandboxID)
}

func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("healthcheck status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("preview healthcheck failed for %s: %w", url, lastErr)
}

func previewStartupTimeout() time.Duration {
	if raw := strings.TrimSpace(os.Getenv("VES_PREVIEW_STARTUP_TIMEOUT")); raw != "" {
		if timeout, err := time.ParseDuration(raw); err == nil && timeout > 0 {
			return timeout
		}
	}
	if os.Getenv("VES_CODEX_EXTERNAL_SANDBOX") == "1" {
		return 2 * time.Minute
	}
	return 30 * time.Second
}

func rewriteHealthcheckURL(healthcheck, previewURL string) string {
	if healthcheck == "" || previewURL == "" {
		return healthcheck
	}
	for _, marker := range []string{"localhost", "127.0.0.1", "0.0.0.0"} {
		if i := strings.Index(healthcheck, marker); i >= 0 {
			path := "/"
			if j := strings.Index(healthcheck[i+len(marker):], "/"); j >= 0 {
				path = healthcheck[i+len(marker)+j:]
			}
			return strings.TrimRight(previewURL, "/") + path
		}
	}
	return healthcheck
}
