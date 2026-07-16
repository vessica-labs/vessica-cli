package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/dashboard"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func configureRailwayService(ctx context.Context, cfg config.Config, secrets railwaySecrets, controlDatabaseURL, linearToken, linearOAuth, railwayOAuth, githubToken, openAIKey, codexAuthB64, previewOrigin string) error {
	privateKeyPath, err := railwaySSHUserKeyPath(cfg)
	if err != nil {
		return err
	}
	privateKey, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("read Railway SSH identity: %w", err)
	}
	variables := map[string]string{
		"VES_CONTROL_DATABASE_URL": controlDatabaseURL, "VES_STATE_BACKEND": "postgres-url",
		"VES_SANDBOX": "railway", "VES_HOSTED_PROVIDER": "railway", "VES_CONTROL_PLANE_URL": cfg.Hosted.ControlPlaneURL,
		"VES_RAILWAY_CHECKPOINT": cfg.Hosted.WorkerCheckpoint,
		"VES_RUNNER":             cfg.Runner.Default, "VES_RUNNER_MODEL": cfg.Runner.Model,
		"VES_RUNNER_REASONING_EFFORT": cfg.Runner.ReasoningEffort, "VES_TRACKER_PROVIDER": cfg.Tracker.Provider,
		"VES_LINEAR_TEAM_ID": cfg.Tracker.TeamID, "VES_LINEAR_TODO_STATE_ID": cfg.Tracker.TodoStateID,
		"VES_LINEAR_WIP_STATE_ID": cfg.Tracker.WIPStateID, "VES_LINEAR_DONE_STATE_ID": cfg.Tracker.DoneStateID,
		"VES_LINEAR_BLOCKED_STATE_ID": cfg.Tracker.BlockedStateID, "VES_LINEAR_TRIGGER_LABEL": cfg.Tracker.TriggerLabel,
		"VES_RAILWAY_POSTGRES_SERVICE_ID": cfg.Hosted.PostgresServiceID,
		"VES_WORKER_DOWNLOAD_TOKEN":       secrets.WorkerToken, "VES_CONTROL_PLANE_API_TOKEN": secrets.ServiceToken,
		"VES_LINEAR_WEBHOOK_SECRET": secrets.WebhookSecret, "LINEAR_API_KEY": linearToken,
		"GITHUB_TOKEN": githubToken, "OPENAI_API_KEY": openAIKey, "RAILWAY_TOKEN": secrets.RuntimeToken,
		"VES_LINEAR_OAUTH_JSON": linearOAuth, "VES_RAILWAY_OAUTH_JSON": railwayOAuth,
		"VES_CREDENTIAL_ENCRYPTION_KEY": secrets.CredentialKey, "VES_CODEX_AUTH_B64": codexAuthB64,
		"VES_RAILWAY_SSH_PRIVATE_KEY": string(privateKey),
		"VES_WORKSPACE_ID":            cfg.Knowledge.WorkspaceID,
		"VES_KNOWLEDGE_MODE":          "hosted", "VES_KNOWLEDGE_WORKSPACE_ID": cfg.Knowledge.WorkspaceID,
		"VES_KNOWLEDGE_ENDPOINT": cfg.Knowledge.Endpoint, "VES_KNOWLEDGE_TOKEN": secrets.KnowledgeToken,
		"VES_DASHBOARD_ENABLED": "true", "VES_DASHBOARD_ORIGIN": cfg.Hosted.ControlPlaneURL, "VES_PREVIEW_ORIGIN": previewOrigin,
		"VES_GITHUB_OAUTH_CLIENT_ID": firstNonEmpty(os.Getenv("VES_GITHUB_OAUTH_CLIENT_ID"), dashboard.DefaultGitHubClientID),
	}
	for key, value := range variables {
		if err := setRailwayVariable(ctx, cfg, key, value); err != nil {
			return err
		}
	}
	return nil
}

func configureRailwayControlPlaneDeploy(ctx context.Context, cfg config.Config) error {
	_, err := runRailway(ctx, "", nil,
		"environment", "edit",
		"--project", cfg.Hosted.ProjectID,
		"--environment", cfg.Hosted.EnvironmentID,
		"--service-config", cfg.Hosted.ServiceID, "deploy.preDeployCommand", "ves control-plane migrate",
		"--message", "Configure Vessica database migration",
		"--json",
	)
	if err != nil {
		return fmt.Errorf("configure Railway control-plane migration: %w", err)
	}
	return nil
}

func resolvedTriggerLabel(requested, existing string) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	return "Vessica"
}

func resolveLinearConfig(discovery *tracker.LinearDiscovery, opts railwayUpOptions) (tracker.LinearTeam, map[string]string, error) {
	if discovery == nil || len(discovery.Teams) == 0 {
		return tracker.LinearTeam{}, nil, fmt.Errorf("Linear workspace has no teams")
	}
	team := discovery.Teams[0]
	if opts.Team != "" {
		found := false
		for _, candidate := range discovery.Teams {
			if strings.EqualFold(opts.Team, candidate.ID) || strings.EqualFold(opts.Team, candidate.Key) || strings.EqualFold(opts.Team, candidate.Name) {
				team, found = candidate, true
				break
			}
		}
		if !found {
			return tracker.LinearTeam{}, nil, fmt.Errorf("Linear team %q not found", opts.Team)
		}
	}
	resolve := func(value, fallbackType string, optional bool) (string, error) {
		if value == "" && optional {
			return "", nil
		}
		for _, candidate := range discovery.States[team.ID] {
			if strings.EqualFold(value, candidate.ID) || strings.EqualFold(value, candidate.Name) || (value == "" && candidate.Type == fallbackType) {
				return candidate.ID, nil
			}
		}
		return "", fmt.Errorf("Linear state %q not found for team %s", value, team.Name)
	}
	todo, err := resolve(opts.TodoState, "unstarted", false)
	if err != nil {
		return team, nil, err
	}
	wip, err := resolve(opts.WIPState, "started", false)
	if err != nil {
		return team, nil, err
	}
	done, err := resolve(opts.DoneState, "completed", false)
	if err != nil {
		return team, nil, err
	}
	blocked, err := resolve(opts.BlockedState, "canceled", true)
	return team, map[string]string{"todo": todo, "wip": wip, "done": done, "blocked": blocked}, err
}

func railwayPath() string {
	if path, err := exec.LookPath("railway"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".railway", "bin", "railway")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "railway"
}

func ensureRailwayCLI(ctx context.Context) error {
	if railwayPath() != "railway" {
		return verifyRailwayCLIVersion(ctx, railwayPath())
	}
	if path, err := exec.LookPath("railway"); err == nil && path != "" {
		return verifyRailwayCLIVersion(ctx, path)
	}
	tmp, err := os.MkdirTemp("", "vessica-railway-bootstrap-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	installer := filepath.Join(tmp, "install.sh")
	download := exec.CommandContext(ctx, "curl", "-fsSL", "https://railway.com/install.sh", "-o", installer)
	if out, err := download.CombinedOutput(); err != nil {
		return fmt.Errorf("download Railway CLI installer: %w: %s", err, strings.TrimSpace(string(out)))
	}
	install := exec.CommandContext(ctx, "sh", installer)
	if out, err := install.CombinedOutput(); err != nil {
		return fmt.Errorf("install Railway CLI: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if railwayPath() == "railway" {
		if _, err := exec.LookPath("railway"); err != nil {
			return fmt.Errorf("Railway CLI installer completed but railway was not found")
		}
	}
	return verifyRailwayCLIVersion(ctx, railwayPath())
}

func verifyRailwayCLIVersion(ctx context.Context, path string) error {
	output, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("read Railway CLI version: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || !strings.HasPrefix(fields[1], toolchain.RailwayCLIMajor+".") {
		return fmt.Errorf("unsupported Railway CLI version %q; Vessica requires tested major version %s", strings.TrimSpace(string(output)), toolchain.RailwayCLIMajor)
	}
	return nil
}

func runRailway(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	return runRailwayWithAuth(ctx, dir, stdin, true, args...)
}

func runRailwaySession(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	return runRailwayWithAuth(ctx, dir, stdin, false, args...)
}

func runRailwayWithAuth(ctx context.Context, dir string, stdin io.Reader, oauth bool, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, railwayPath(), args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = stdin
	for _, value := range os.Environ() {
		if !oauth && (strings.HasPrefix(value, "RAILWAY_TOKEN=") || strings.HasPrefix(value, "RAILWAY_API_TOKEN=")) {
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env, "RAILWAY_CALLER=vessica-cli", "RAILWAY_AGENT_SESSION=vessica-railway-up")
	if oauth && os.Getenv("RAILWAY_TOKEN") == "" && os.Getenv("RAILWAY_API_TOKEN") == "" {
		if token, err := auth.Token("railway"); err == nil && token != "" {
			cmd.Env = append(cmd.Env, "RAILWAY_API_TOKEN="+token)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("railway %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func runRailwaySSHKeys(ctx context.Context, args ...string) ([]byte, error) {
	command := append([]string{"ssh", "keys"}, args...)
	output, oauthErr := runRailway(ctx, "", nil, command...)
	if oauthErr == nil || !strings.Contains(strings.ToLower(oauthErr.Error()), "unauthorized") {
		return output, oauthErr
	}
	output, sessionErr := runRailwaySession(ctx, "", nil, command...)
	if sessionErr != nil {
		return nil, fmt.Errorf("Railway OAuth tokens are not accepted by the SSH-key endpoint and the Railway CLI session is unavailable; run `railway login` once, then retry: %w", sessionErr)
	}
	return output, nil
}

func setRailwayVariable(ctx context.Context, cfg config.Config, key, value string) error {
	return setRailwayVariableForService(ctx, cfg, cfg.Hosted.ServiceID, key, value)
}

func setRailwayVariableForService(ctx context.Context, cfg config.Config, serviceID, key, value string) error {
	if value == "" {
		_, err := runRailway(ctx, "", nil, "variable", "delete", key, "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", serviceID, "--json")
		if err == nil || strings.Contains(strings.ToLower(err.Error()), "not found") {
			return nil
		}
		return fmt.Errorf("delete empty Railway variable %s: %w", key, err)
	}
	_, err := runRailway(ctx, "", strings.NewReader(value), "variable", "set", key, "--stdin", "--skip-deploys", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", serviceID, "--json")
	if err != nil {
		return fmt.Errorf("set Railway variable %s: %w", key, err)
	}
	return nil
}

func objectID(raw []byte) (string, error) { return objectString(raw, "id") }

type railwayServiceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
