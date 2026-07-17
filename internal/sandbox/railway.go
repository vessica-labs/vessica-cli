package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRailwayIdleTimeoutMinutes = 120

// RailwaySandbox implements Sandbox with Railway's ephemeral agent sandboxes.
// The Railway CLI is the transport so the same implementation works with a
// user's interactive login locally and a refreshed OAuth token in the control plane.
type RailwaySandbox struct {
	cli           string
	projectID     string
	environmentID string
	sandboxID     string
	opts          CreateOpts
	status        string
	previewURL    string
	forwardMu     sync.Mutex
	forwardCancel context.CancelFunc
	forwardDone   <-chan struct{}
	apiToken      string
	cliHome       string
	persistCLI    func() error
}

func NewRailway(cli, projectID, environmentID, sandboxID string) *RailwaySandbox {
	if cli == "" {
		cli = "railway"
	}
	return &RailwaySandbox{cli: cli, projectID: projectID, environmentID: environmentID, sandboxID: sandboxID, status: "pending"}
}

func (r *RailwaySandbox) ID() string               { return r.sandboxID }
func (r *RailwaySandbox) ContainerID() string      { return r.sandboxID }
func (r *RailwaySandbox) Workdir() string          { return "/workspace" }
func (r *RailwaySandbox) SetAPIToken(token string) { r.apiToken = strings.TrimSpace(token) }

func (r *RailwaySandbox) SetCLIHome(home string) { r.cliHome = strings.TrimSpace(home) }

func (r *RailwaySandbox) SetSessionPersist(persist func() error) { r.persistCLI = persist }

func (r *RailwaySandbox) UsesCLISession() bool { return r.cliHome != "" }

func (r *RailwaySandbox) baseArgs() []string {
	return []string{"sandbox", "-p", r.projectID, "-e", r.environmentID}
}

func (r *RailwaySandbox) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.cli, args...)
	for _, value := range os.Environ() {
		if (r.apiToken != "" || r.cliHome != "") && (strings.HasPrefix(value, "RAILWAY_TOKEN=") || strings.HasPrefix(value, "RAILWAY_API_TOKEN=")) {
			continue
		}
		if r.cliHome != "" && strings.HasPrefix(value, "HOME=") {
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "RAILWAY_CALLER=vessica-control-plane")
	if r.cliHome != "" {
		cmd.Env = append(cmd.Env, "HOME="+r.cliHome)
	}
	if r.apiToken != "" {
		cmd.Env = append(cmd.Env, "RAILWAY_API_TOKEN="+r.apiToken)
	}
	return cmd
}

func (r *RailwaySandbox) Create(ctx context.Context, opts CreateOpts) error {
	r.opts = opts
	args := append(r.baseArgs(), "create", "--private-network", "--json")
	minutes := int(time.Until(opts.ExpiresAt).Minutes())
	if minutes <= 0 {
		minutes = maxRailwayIdleTimeoutMinutes
	}
	if minutes > maxRailwayIdleTimeoutMinutes {
		minutes = maxRailwayIdleTimeoutMinutes
	}
	args = append(args, "--idle-timeout-minutes", strconv.Itoa(minutes))
	if checkpoint := strings.TrimSpace(opts.Env["VES_RAILWAY_CHECKPOINT"]); checkpoint != "" {
		args = append(args, "--checkpoint", checkpoint)
	}
	for key, value := range opts.Env {
		if key == "VES_RAILWAY_CHECKPOINT" {
			continue
		}
		args = append(args, "--variable", key+"="+value)
	}
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		_ = r.persistSession()
		return fmt.Errorf("railway sandbox create: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := r.persistSession(); err != nil {
		return fmt.Errorf("persist Railway CLI session after sandbox create: %w", err)
	}
	id, err := railwayObjectID(stdout.Bytes())
	if err != nil {
		return fmt.Errorf("railway sandbox create: %w", err)
	}
	r.sandboxID = id
	r.status = "running"
	return nil
}

func railwayObjectID(raw []byte) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("invalid JSON response: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			for _, key := range []string{"id", "sandboxId", "sandbox_id"} {
				if id, ok := x[key].(string); ok && id != "" {
					return id
				}
			}
			for _, child := range x {
				if id := find(child); id != "" {
					return id
				}
			}
		case []any:
			for _, child := range x {
				if id := find(child); id != "" {
					return id
				}
			}
		}
		return ""
	}
	if id := find(value); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("response did not include a sandbox id")
}

func (r *RailwaySandbox) Start(context.Context) error {
	if r.sandboxID == "" {
		return ErrNotRunning
	}
	r.status = "running"
	return nil
}

func (r *RailwaySandbox) Exec(ctx context.Context, argv []string, stdout, stderr io.Writer) (int, error) {
	if r.sandboxID == "" {
		return 1, ErrNotRunning
	}
	args := append(r.baseArgs(), "exec", "--id", r.sandboxID, "--timeout", "7200", "--")
	args = append(args, argv...)
	cmd := r.command(ctx, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	persistErr := r.persistSession()
	if err == nil {
		if persistErr != nil {
			return 1, fmt.Errorf("persist Railway CLI session after sandbox exec: %w", persistErr)
		}
		return 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), err
	}
	return 1, err
}

func (r *RailwaySandbox) Stream(context.Context, io.Writer, io.Writer) error { return nil }

func (r *RailwaySandbox) StartPreview(ctx context.Context, command string, port int, healthcheck string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("preview command is empty")
	}
	path := strings.TrimSpace(healthcheck)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	script := strings.Join([]string{
		"cd /workspace",
		"if test -f .vessica-preview.pid && kill -0 $(cat .vessica-preview.pid) 2>/dev/null; then exit 0; fi",
		"nohup bash -lc " + shellQuote(command) + " >.vessica-preview.log 2>&1 </dev/null &",
		"echo $! >.vessica-preview.pid",
		"for attempt in $(seq 1 60); do curl -fsS " + shellQuote(url) + " >/dev/null 2>&1 && exit 0; sleep 1; done",
		"tail -n 100 .vessica-preview.log >&2 || true",
		"exit 1",
	}, "\n")
	if code, err := r.Exec(ctx, []string{"bash", "-lc", script}, io.Discard, io.Discard); err != nil || code != 0 {
		return "", fmt.Errorf("start railway preview: exit %d: %w", code, err)
	}
	r.previewURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	return r.previewURL, nil
}

func (r *RailwaySandbox) StopPreview(ctx context.Context) error {
	_, _ = r.Exec(ctx, []string{"bash", "-lc", "test -f .vessica-preview.pid && kill $(cat .vessica-preview.pid) 2>/dev/null || true"}, io.Discard, io.Discard)
	r.previewURL = ""
	return r.StopForward()
}

func (r *RailwaySandbox) RefreshLease(context.Context, time.Time) error { return nil }
func (r *RailwaySandbox) PreviewURL() string                            { return r.previewURL }

func (r *RailwaySandbox) Destroy(ctx context.Context) error {
	_ = r.StopForward()
	if r.sandboxID == "" {
		return nil
	}
	args := append(r.baseArgs(), "destroy", "--id", r.sandboxID)
	out, err := r.command(ctx, args...).CombinedOutput()
	persistErr := r.persistSession()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "not found") {
		return fmt.Errorf("railway sandbox destroy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if persistErr != nil {
		return fmt.Errorf("persist Railway CLI session after sandbox destroy: %w", persistErr)
	}
	r.status = "destroyed"
	return nil
}

func (r *RailwaySandbox) Status(ctx context.Context) (string, error) {
	if r.sandboxID == "" {
		return "pending", nil
	}
	args := append(r.baseArgs(), "list", "--all", "--json")
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	persistErr := r.persistSession()
	if err != nil {
		return "", fmt.Errorf("railway sandbox list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if persistErr != nil {
		return "", fmt.Errorf("persist Railway CLI session after sandbox list: %w", persistErr)
	}
	var list []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &list); err != nil {
		return "", err
	}
	for _, item := range list {
		if fmt.Sprint(item["id"]) == r.sandboxID {
			for _, key := range []string{"status", "state"} {
				if value := strings.ToLower(fmt.Sprint(item[key])); value != "" && value != "<nil>" {
					r.status = value
					return value, nil
				}
			}
		}
	}
	return "missing", nil
}

func (r *RailwaySandbox) persistSession() error {
	if r.persistCLI == nil {
		return nil
	}
	return r.persistCLI()
}

// HealthyURL is exported for the preview broker's startup checks.
func HealthyURL(ctx context.Context, url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("URL did not become healthy: %s", url)
}
