package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
	"github.com/vessica-labs/vessica-cli/internal/version"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

type railwaySecrets struct {
	RuntimeToken, APIToken, WorkerToken, WebhookSecret, WebhookID, CredentialKey, KnowledgeToken, KnowledgeAdminToken string
}

type railwayUpOptions struct {
	Name, Workspace, Source, Image, RuntimeToken, LinearToken, GitHubToken, OpenAIKey  string
	Team, TodoState, WIPState, DoneState, BlockedState, TriggerLabel, WorkerCheckpoint string
	KnowledgeImage, KnowledgeSource, EmbeddingAPIKey, EmbeddingAPIKeyEnv               string
}

func newRailwayCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "railway", Short: "Provision and operate the hosted Railway control plane"}
	var opts railwayUpOptions
	up := &cobra.Command{
		Use: "up", Short: "Provision the control plane, Postgres, Linear webhook, and Railway sandbox access",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspaceWithoutGC(); err != nil {
				return app.Printer.Fail("not_initialized", err.Error(), "run ves init first")
			}
			defer app.closeDB()
			result, err := railwayUp(cmd.Context(), app, opts)
			if err != nil {
				return app.Printer.Fail("railway_up_failed", err.Error(), "run ves railway status and ves railway logs for details")
			}
			return app.Printer.Success(result)
		},
	}
	up.Flags().StringVar(&opts.Name, "name", "vessica-control-plane", "Railway project name")
	up.Flags().StringVar(&opts.Workspace, "workspace", "", "Railway workspace id or name")
	up.Flags().StringVar(&opts.Source, "source", "", "deploy control-plane source from this directory")
	up.Flags().StringVar(&opts.Image, "image", "", "published control-plane image")
	up.Flags().StringVar(&opts.RuntimeToken, "railway-token", "", "headless fallback project token for the hosted service")
	up.Flags().StringVar(&opts.LinearToken, "linear-token", "", "headless fallback Linear API key")
	up.Flags().StringVar(&opts.GitHubToken, "github-token", "", "GitHub token with repository and PR access")
	up.Flags().StringVar(&opts.OpenAIKey, "openai-api-key", "", "headless fallback OpenAI API key for hosted Codex")
	up.Flags().StringVar(&opts.Team, "linear-team", "", "Linear team id, key, or name")
	up.Flags().StringVar(&opts.TodoState, "todo-state", "Todo", "Linear Todo state id or name")
	up.Flags().StringVar(&opts.WIPState, "wip-state", "In Progress", "Linear WIP state id or name")
	up.Flags().StringVar(&opts.DoneState, "done-state", "Done", "Linear Done state id or name")
	up.Flags().StringVar(&opts.BlockedState, "blocked-state", "", "optional Linear blocked state id or name")
	up.Flags().StringVar(&opts.TriggerLabel, "trigger-label", "", "only process issues with this label")
	up.Flags().StringVar(&opts.WorkerCheckpoint, "worker-checkpoint", "", "Railway sandbox checkpoint with worker prerequisites")
	up.Flags().StringVar(&opts.KnowledgeImage, "knowledge-image", "", "knowledge-server OCI image override (resolved to an immutable digest)")
	up.Flags().StringVar(&opts.KnowledgeSource, "knowledge-source", "", "development-only knowledge-server source directory")
	up.Flags().StringVar(&opts.EmbeddingAPIKey, "embedding-api-key", "", "embedding provider key required by hosted knowledge")
	up.Flags().StringVar(&opts.EmbeddingAPIKeyEnv, "embedding-api-key-env", "EMBEDDING_API_KEY", "environment variable containing the hosted embedding provider key")
	cmd.AddCommand(up, newRailwayStatusCmd(app), newRailwayLogsCmd(app), newRailwayApproveCmd(app), newRailwayDownCmd(app))
	return cmd
}

func newRailwayStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show hosted control-plane and deployment status", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(); err != nil {
			return err
		}
		defer app.closeDB()
		cfg := app.Config
		secrets, _ := loadRailwaySecrets(app.Root)
		result := map[string]any{"configured": cfg.Hosted.ProjectID != "", "hosted": cfg.Hosted}
		if cfg.Hosted.ControlPlaneURL != "" && secrets.APIToken != "" {
			var remote any
			if err := hostedRequest(cmd.Context(), http.MethodGet, cfg.Hosted.ControlPlaneURL+"/api/v1/status", secrets.APIToken, nil, &remote); err == nil {
				result["control_plane"] = remote
			} else {
				result["control_plane_error"] = err.Error()
			}
		}
		if cfg.Hosted.ProjectID != "" {
			if raw, err := runRailway(cmd.Context(), "", nil, "deployment", "list", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--limit", "5", "--json"); err == nil {
				var deployments any
				_ = json.Unmarshal(raw, &deployments)
				result["deployments"] = deployments
			}
			if cfg.Knowledge.ServiceID != "" {
				if raw, err := runRailway(cmd.Context(), "", nil, "deployment", "list", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--limit", "5", "--json"); err == nil {
					var deployments any
					_ = json.Unmarshal(raw, &deployments)
					result["knowledge_deployments"] = deployments
				}
			}
		}
		return app.Printer.Success(result)
	}}
}

func newRailwayLogsCmd(app *App) *cobra.Command {
	var lines int
	cmd := &cobra.Command{Use: "logs", Short: "Read control-plane runtime logs", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(); err != nil {
			return err
		}
		defer app.closeDB()
		cfg := app.Config
		if cfg.Hosted.ProjectID == "" {
			return fmt.Errorf("Railway control plane is not configured")
		}
		out, err := runRailway(cmd.Context(), "", nil, "logs", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--lines", fmt.Sprint(lines))
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(out)
		return err
	}}
	cmd.Flags().IntVar(&lines, "lines", 100, "number of log lines")
	return cmd
}

func newRailwayApproveCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "approve <run_id>", Short: "Approve a hosted run and merge its draft PR", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(); err != nil {
			return err
		}
		defer app.closeDB()
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return err
		}
		var result any
		url := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + args[0] + "/approve"
		if err := hostedRequest(cmd.Context(), http.MethodPost, url, secrets.APIToken, nil, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
}

func newRailwayDownCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "down", Short: "Delete the hosted Railway project", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.requireYes("delete the Railway control-plane project"); err != nil {
			return err
		}
		if err := app.loadWorkspaceWithoutGC(); err != nil {
			return err
		}
		defer app.closeDB()
		projectID := app.Config.Hosted.ProjectID
		if projectID == "" {
			return app.Printer.Success(map[string]any{"deleted": false, "reason": "not configured"})
		}
		if keyPath, err := railwaySSHUserKeyPath(app.Config); err == nil {
			if out, err := exec.CommandContext(cmd.Context(), "ssh-keygen", "-lf", keyPath+".pub", "-E", "sha256").Output(); err == nil {
				fields := strings.Fields(string(out))
				if len(fields) > 1 {
					_, _ = runRailwaySSHKeys(cmd.Context(), "remove", fields[1], "--workspace", app.Config.Hosted.WorkspaceID)
				}
			}
			_ = os.Remove(keyPath)
			_ = os.Remove(keyPath + ".pub")
		}
		if _, err := runRailway(cmd.Context(), "", nil, "delete", "--project", projectID, "--yes", "--json"); err != nil {
			return err
		}
		app.Config.Hosted = config.HostedConfig{}
		if err := config.Save(app.Root, app.Config); err != nil {
			return err
		}
		_ = os.Remove(railwaySecretsPath(app.Root))
		return app.Printer.Success(map[string]any{"deleted": true, "project_id": projectID})
	}}
}

func railwayUp(ctx context.Context, app *App, opts railwayUpOptions) (map[string]any, error) {
	linearOAuth, _ := auth.MarshalOAuth("linear")
	linearToken := firstNonEmpty(opts.LinearToken, os.Getenv("LINEAR_API_KEY"))
	if linearToken == "" {
		linearToken, _ = auth.Token("linear")
	}
	githubToken := firstNonEmpty(opts.GitHubToken, os.Getenv("GITHUB_TOKEN"))
	if githubToken == "" {
		githubToken, _ = auth.Token("github")
	}
	openAIKey := firstNonEmpty(opts.OpenAIKey, os.Getenv("OPENAI_API_KEY"))
	if openAIKey == "" {
		openAIKey, _ = auth.Token("openai")
	}
	codeAuth, _ := auth.CodexAuthJSON()
	codexAuthB64 := ""
	if len(codeAuth) > 0 {
		codexAuthB64 = base64.RawStdEncoding.EncodeToString(codeAuth)
	}
	railwayOAuth, _ := auth.MarshalOAuth("railway")
	secrets, _ := loadRailwaySecrets(app.Root)
	runtimeToken := firstNonEmpty(opts.RuntimeToken, os.Getenv("RAILWAY_TOKEN"), secrets.RuntimeToken)
	cfg := app.Config
	cfg.Hosted.Provider, cfg.Hosted.WorkerCheckpoint = "railway", opts.WorkerCheckpoint
	workDir, err := os.MkdirTemp("", "ves-railway-up-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	if cfg.Hosted.ProjectID == "" {
		if err := createRailwayResources(ctx, workDir, &cfg, opts); err != nil {
			return nil, err
		}
		if err := config.Save(app.Root, cfg); err != nil {
			return nil, err
		}
		app.Config = cfg
	}
	if err := reconcileRailwayResourceIDs(ctx, &cfg); err != nil {
		return nil, err
	}
	if err := ensureRailwaySSHIdentity(ctx, app.Root, cfg); err != nil {
		return nil, err
	}
	if cfg.Hosted.WorkerCheckpoint == "" {
		checkpoint, err := ensureRailwayWorkerCheckpoint(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfg.Hosted.WorkerCheckpoint = checkpoint
	}
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	app.Config = cfg
	if secrets.APIToken == "" {
		secrets.APIToken = randomSecret(32)
	}
	if secrets.WorkerToken == "" {
		secrets.WorkerToken = randomSecret(32)
	}
	if secrets.WebhookSecret == "" {
		secrets.WebhookSecret = randomSecret(32)
	}
	if secrets.CredentialKey == "" {
		secrets.CredentialKey = base64.RawStdEncoding.EncodeToString(randomBytes(32))
	}
	if secrets.KnowledgeToken == "" {
		secrets.KnowledgeToken = randomSecret(32)
	}
	if secrets.KnowledgeAdminToken == "" {
		secrets.KnowledgeAdminToken = randomSecret(32)
	}
	secrets.RuntimeToken = runtimeToken
	if err := saveRailwaySecrets(app.Root, secrets); err != nil {
		return nil, err
	}
	var missing []string
	if runtimeToken == "" && railwayOAuth == "" {
		missing = append(missing, "Railway browser login")
	}
	if linearToken == "" && linearOAuth == "" {
		missing = append(missing, "Linear browser login")
	}
	if githubToken == "" {
		missing = append(missing, "GitHub token")
	}
	if openAIKey == "" && codexAuthB64 == "" {
		missing = append(missing, "Codex browser login")
	}
	embeddingKey := opts.EmbeddingAPIKey
	if embeddingKey == "" && opts.EmbeddingAPIKeyEnv != "" {
		embeddingKey = os.Getenv(opts.EmbeddingAPIKeyEnv)
	}
	if embeddingKey == "" {
		missing = append(missing, "embedding provider API key")
	}
	if len(missing) > 0 {
		_, _ = app.DB.UpsertControlPlaneDeployment(ctx, &state.ControlPlaneDeployment{
			Provider: "railway", ProjectID: cfg.Hosted.ProjectID, EnvironmentID: cfg.Hosted.EnvironmentID,
			ServiceID: cfg.Hosted.ServiceID, PostgresServiceID: cfg.Hosted.PostgresServiceID,
			Version: version.Version, Status: "awaiting_credentials",
		})
		return map[string]any{
			"status": "awaiting_credentials", "project_id": cfg.Hosted.ProjectID,
			"environment_id": cfg.Hosted.EnvironmentID, "service_id": cfg.Hosted.ServiceID,
			"postgres_service_id": cfg.Hosted.PostgresServiceID, "missing": missing,
			"project_url": "https://railway.com/project/" + cfg.Hosted.ProjectID,
			"next":        "Run ves auth login railway, ves auth login linear, ves auth login github, and ves auth login codex; then rerun ves railway up.",
		}, nil
	}
	if linearToken == "" {
		linearToken, err = auth.Token("linear")
		if err != nil {
			return nil, err
		}
	}
	linear := tracker.NewLinearClient(linearToken)
	discovery, err := linear.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover Linear workspace: %w", err)
	}
	team, states, err := resolveLinearConfig(discovery, opts)
	if err != nil {
		return nil, err
	}
	cfg.Tracker.Provider = "linear"
	cfg.Tracker.TeamID, cfg.Tracker.TodoStateID = team.ID, states["todo"]
	cfg.Tracker.WIPStateID, cfg.Tracker.DoneStateID = states["wip"], states["done"]
	cfg.Tracker.BlockedStateID, cfg.Tracker.TriggerLabel = states["blocked"], opts.TriggerLabel
	if cfg.Tracker.TriggerLabel != "" {
		if _, err := linear.EnsureIssueLabel(ctx, team.ID, cfg.Tracker.TriggerLabel); err != nil {
			return nil, fmt.Errorf("ensure Linear trigger label: %w", err)
		}
	}
	if err := ensureRailwayDomain(ctx, workDir, &cfg); err != nil {
		return nil, err
	}
	if err := ensureRailwayKnowledge(ctx, workDir, app, &cfg, opts, secrets.KnowledgeToken, secrets.KnowledgeAdminToken, embeddingKey); err != nil {
		return nil, err
	}
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	hostedLinearToken := linearToken
	if linearOAuth != "" {
		hostedLinearToken = ""
	}
	if err := configureRailwayService(ctx, app.Root, cfg, secrets, hostedLinearToken, linearOAuth, railwayOAuth, githubToken, openAIKey, codexAuthB64); err != nil {
		return nil, err
	}
	source := opts.Source
	if source == "" && opts.Image == "" {
		if moduleIsVessica(app.Root) {
			source = app.Root
		} else {
			return nil, fmt.Errorf("no published control-plane image configured; pass --source /path/to/vessica-cli")
		}
	}
	previousDeploymentID := ""
	if latest, err := latestRailwayDeployment(ctx, cfg); err == nil {
		previousDeploymentID = latest.ID
	}
	if source != "" {
		if _, err := runRailway(ctx, source, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--detach", "--json", "--message", "Vessica "+version.Version); err != nil {
			return nil, err
		}
	} else {
		_, _ = runRailway(ctx, "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--yes")
	}
	if err := waitForRailwayDeployment(ctx, cfg, previousDeploymentID, 8*time.Minute); err != nil {
		return nil, err
	}
	if err := waitForHostedHealth(ctx, cfg.Hosted.ControlPlaneURL+"/readyz", 8*time.Minute); err != nil {
		return nil, err
	}
	if secrets.WebhookID == "" {
		webhook, err := linear.CreateWebhook(ctx, team.ID, cfg.Hosted.ControlPlaneURL+"/webhooks/linear", secrets.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("create Linear webhook: %w", err)
		}
		secrets.WebhookID = webhook.ID
		if err := saveRailwaySecrets(app.Root, secrets); err != nil {
			return nil, err
		}
	}
	if err := setRailwayVariable(ctx, cfg, "VES_LINEAR_WEBHOOK_ID", secrets.WebhookID); err != nil {
		return nil, err
	}
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	app.Config = cfg
	_, _ = app.DB.UpsertControlPlaneDeployment(ctx, &state.ControlPlaneDeployment{
		Provider: "railway", ProjectID: cfg.Hosted.ProjectID, EnvironmentID: cfg.Hosted.EnvironmentID,
		ServiceID: cfg.Hosted.ServiceID, PostgresServiceID: cfg.Hosted.PostgresServiceID,
		PublicURL: cfg.Hosted.ControlPlaneURL, Version: version.Version, Status: "running",
	})
	return map[string]any{"status": "running", "project_id": cfg.Hosted.ProjectID, "environment_id": cfg.Hosted.EnvironmentID,
		"service_id": cfg.Hosted.ServiceID, "postgres_service_id": cfg.Hosted.PostgresServiceID,
		"control_plane_url": cfg.Hosted.ControlPlaneURL, "webhook_id": secrets.WebhookID,
		"knowledge_endpoint": cfg.Knowledge.Endpoint, "knowledge_service_id": cfg.Knowledge.ServiceID,
		"linear_team": team.Name, "todo_state_id": cfg.Tracker.TodoStateID}, nil
}

func createRailwayResources(ctx context.Context, workDir string, cfg *config.Config, opts railwayUpOptions) error {
	args := []string{"init", "--name", opts.Name, "--json"}
	if opts.Workspace != "" {
		args = append(args, "--workspace", opts.Workspace)
	}
	raw, err := runRailway(ctx, workDir, nil, args...)
	if err != nil {
		return err
	}
	cfg.Hosted.ProjectID, err = objectID(raw)
	if err != nil {
		return err
	}
	cfg.Hosted.EnvironmentID = "production"
	serviceArgs := []string{"add", "--service", "control-plane", "--json"}
	if opts.Image != "" {
		serviceArgs = []string{"add", "--image", opts.Image, "--service", "control-plane", "--json"}
		cfg.Hosted.ControlPlaneImage = opts.Image
	}
	raw, err = runRailway(ctx, workDir, nil, serviceArgs...)
	if err != nil {
		return err
	}
	cfg.Hosted.ServiceID, err = objectID(raw)
	if err != nil {
		return err
	}
	raw, err = runRailway(ctx, workDir, nil, "add", "--database", "postgres", "--json")
	if err != nil {
		return err
	}
	cfg.Hosted.PostgresServiceID, _ = objectID(raw)
	return nil
}

func reconcileRailwayResourceIDs(ctx context.Context, cfg *config.Config) error {
	raw, err := runRailway(ctx, "", nil, "status", "--project", cfg.Hosted.ProjectID, "--environment", firstNonEmpty(cfg.Hosted.EnvironmentID, "production"), "--json")
	if err != nil {
		return err
	}
	var project struct {
		WorkspaceID  string `json:"workspaceId"`
		Environments struct {
			Edges []struct {
				Node struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"environments"`
		Services struct {
			Edges []struct {
				Node struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &project); err != nil {
		return err
	}
	cfg.Hosted.WorkspaceID = project.WorkspaceID
	for _, edge := range project.Environments.Edges {
		if edge.Node.Name == "production" || edge.Node.ID == cfg.Hosted.EnvironmentID {
			cfg.Hosted.EnvironmentID = edge.Node.ID
			break
		}
	}
	for _, edge := range project.Services.Edges {
		if edge.Node.ID == cfg.Knowledge.PostgresServiceID {
			cfg.Knowledge.PostgresServiceName = edge.Node.Name
		}
		switch strings.ToLower(edge.Node.Name) {
		case "control-plane":
			cfg.Hosted.ServiceID = edge.Node.ID
		case "postgres":
			cfg.Hosted.PostgresServiceID = edge.Node.ID
		case "knowledge-server":
			cfg.Knowledge.ServiceID = edge.Node.ID
		case "knowledge-postgres":
			cfg.Knowledge.PostgresServiceID = edge.Node.ID
			cfg.Knowledge.PostgresServiceName = edge.Node.Name
		}
	}
	if cfg.Hosted.EnvironmentID == "" || cfg.Hosted.ServiceID == "" || cfg.Hosted.PostgresServiceID == "" {
		return fmt.Errorf("Railway project is missing production, control-plane, or Postgres resources")
	}
	return nil
}

func ensureRailwaySSHIdentity(ctx context.Context, root string, cfg config.Config) error {
	workspacePath := railwaySSHPrivateKeyPath(root)
	userPath, err := railwaySSHUserKeyPath(cfg)
	if err != nil {
		return err
	}
	if _, err := os.Stat(userPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
			return err
		}
		comment := "vessica-control-plane-" + cfg.Hosted.ProjectID
		out, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", userPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("generate Railway SSH identity: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	privateKey, err := os.ReadFile(userPath)
	if err != nil {
		return err
	}
	publicKey, err := os.ReadFile(userPath + ".pub")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(workspacePath, privateKey, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(workspacePath+".pub", publicKey, 0o644); err != nil {
		return err
	}
	if cfg.Hosted.WorkspaceID == "" {
		return fmt.Errorf("Railway workspace id is unavailable")
	}
	name := "vessica-control-plane-" + cfg.Hosted.ProjectID[:8]
	list, err := runRailwaySSHKeys(ctx, "list", "--workspace", cfg.Hosted.WorkspaceID)
	if err != nil {
		return err
	}
	if !strings.Contains(string(list), name) {
		if _, err := runRailwaySSHKeys(ctx, "add", "--key", userPath, "--name", name, "--workspace", cfg.Hosted.WorkspaceID); err != nil {
			return err
		}
	}
	return nil
}

func ensureRailwayWorkerCheckpoint(ctx context.Context, cfg config.Config) (string, error) {
	name := "vessica-worker-" + strings.ReplaceAll(version.Version, ".", "-") + "-toolchain-3"
	base := []string{"sandbox", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID}
	raw, err := runRailway(ctx, "", nil, append(base, "checkpoint", "list", "--json")...)
	if err == nil && bytes.Contains(raw, []byte(name)) {
		return name, nil
	}
	raw, err = runRailway(ctx, "", nil, append(base, "create", "--private-network", "--idle-timeout-minutes", "30", "--json")...)
	if err != nil {
		return "", err
	}
	sandboxID, err := objectID(raw)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = runRailway(context.Background(), "", nil, append(base, "destroy", "--id", sandboxID)...)
	}()
	install := "set -e; command -v pnpm >/dev/null || npm install -g pnpm@11.9.0; npm install -g @openai/codex@latest playwright@latest; export NODE_PATH=$(npm root -g); playwright install --with-deps chromium"
	execArgs := append(base, "exec", "--id", sandboxID, "--timeout", "1200", "--", "bash", "-lc", install)
	if _, err := runRailway(ctx, "", nil, execArgs...); err != nil {
		return "", err
	}
	if _, err := runRailway(ctx, "", nil, append(base, "checkpoint", "create", name, "--id", sandboxID, "--json")...); err != nil {
		return "", err
	}
	return name, nil
}

func ensureRailwayDomain(ctx context.Context, workDir string, cfg *config.Config) error {
	if cfg.Hosted.ControlPlaneURL != "" {
		return nil
	}
	raw, err := runRailway(ctx, workDir, nil, "domain", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "-p", "8080", "--json")
	if err != nil {
		return err
	}
	domain, err := objectString(raw, "domain")
	if err != nil {
		return err
	}
	cfg.Hosted.ControlPlaneURL = "https://" + strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	return nil
}

const knowledgeServerVersion = "0.1.4"

func ensureRailwayKnowledge(ctx context.Context, workDir string, app *App, cfg *config.Config, opts railwayUpOptions, token, adminToken, embeddingKey string) error {
	image := opts.KnowledgeImage
	if image == "" && opts.KnowledgeSource == "" {
		image = "ghcr.io/vessica-labs/vessica-knowledge-server:v" + knowledgeServerVersion
		var err error
		image, err = resolveGHCRDigest(ctx, image)
		if err != nil {
			return fmt.Errorf("resolve knowledge-server release image: %w", err)
		}
	}
	if cfg.Knowledge.ServiceID == "" {
		args := []string{"add", "--service", "knowledge-server", "--json"}
		if image != "" {
			args = []string{"add", "--image", image, "--service", "knowledge-server", "--json"}
		}
		raw, err := runRailway(ctx, workDir, nil, args...)
		if err != nil {
			return err
		}
		cfg.Knowledge.ServiceID, err = objectID(raw)
		if err != nil {
			return err
		}
		if err := config.Save(app.Root, *cfg); err != nil {
			return err
		}
	}
	if cfg.Knowledge.PostgresServiceID == "" {
		raw, err := runRailway(ctx, workDir, nil, "add", "--database", "postgres", "--json")
		if err != nil {
			return err
		}
		cfg.Knowledge.PostgresServiceID, err = objectID(raw)
		if err != nil {
			return err
		}
		cfg.Knowledge.PostgresServiceName, err = objectString(raw, "name")
		if err != nil {
			return err
		}
	}
	if cfg.Knowledge.PostgresServiceName == "" {
		return fmt.Errorf("knowledge Postgres service name is unavailable")
	}
	if err := config.Save(app.Root, *cfg); err != nil {
		return err
	}
	if cfg.Knowledge.Endpoint == "" {
		raw, err := runRailway(ctx, workDir, nil, "domain", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "-p", "8080", "--json")
		if err != nil {
			return err
		}
		domain, err := objectString(raw, "domain")
		if err != nil {
			return err
		}
		cfg.Knowledge.Endpoint = "https://" + strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	}
	ws, err := app.DB.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if cfg.Knowledge.WorkspaceID == "" {
		cfg.Knowledge.WorkspaceID = ws.ID
	}
	variables := map[string]string{
		"DATABASE_URL":           "$" + "{{" + cfg.Knowledge.PostgresServiceName + ".DATABASE_URL}}",
		"KNOWLEDGE_API_TOKEN":    token,
		"KNOWLEDGE_EXPORT_TOKEN": adminToken,
		"KNOWLEDGE_WORKSPACE_ID": cfg.Knowledge.WorkspaceID,
		"EMBEDDING_API_KEY":      embeddingKey,
		"EMBEDDING_MODEL":        "text-embedding-3-small",
	}
	for key, value := range variables {
		if err := setRailwayVariableForService(ctx, *cfg, cfg.Knowledge.ServiceID, key, value); err != nil {
			return err
		}
	}
	previous := ""
	if latest, err := latestRailwayDeploymentForService(ctx, *cfg, cfg.Knowledge.ServiceID); err == nil {
		previous = latest.ID
	}
	if opts.KnowledgeSource != "" {
		if _, err := runRailway(ctx, opts.KnowledgeSource, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--detach", "--json", "--message", "Vessica knowledge dev"); err != nil {
			return err
		}
	} else {
		if _, err := runRailway(ctx, "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--yes"); err != nil {
			return err
		}
	}
	if err := waitForRailwayDeploymentForService(ctx, *cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute); err != nil {
		return err
	}
	if err := waitForHostedHealth(ctx, strings.TrimRight(cfg.Knowledge.Endpoint, "/")+"/readyz", 8*time.Minute); err != nil {
		return err
	}
	cfg.Knowledge.Version = knowledgeServerVersion
	cfg.Knowledge.Image = image
	if cfg.Knowledge.Mode != "hosted" {
		if err := promoteKnowledgeAuthority(ctx, app, cfg, token, adminToken); err != nil {
			return err
		}
	}
	return nil
}

func promoteKnowledgeAuthority(ctx context.Context, app *App, cfg *config.Config, token, adminToken string) error {
	lockPath := filepath.Join(app.Root, ".vessica", "state", "knowledge.promote.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("another knowledge promotion is active")
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	ws, err := app.DB.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	local, err := knowledgegateway.OpenForPromotion(app.Root, app.Config, ws.ID)
	if err != nil {
		return err
	}
	snap, err := local.Export(ctx)
	_ = local.Close()
	if err != nil {
		return err
	}
	if err := auth.Login("knowledge", token, "Railway hosted knowledge"); err != nil {
		return err
	}
	if err := auth.Login("knowledge-export", adminToken, "Railway hosted knowledge export"); err != nil {
		return err
	}
	next := *cfg
	next.Knowledge.Mode = "hosted"
	remote, err := knowledgegateway.Open(app.Root, next, snap.WorkspaceID)
	if err != nil {
		return err
	}
	defer remote.Close()
	if err := remote.Import(ctx, snap); err != nil {
		return err
	}
	check, err := remote.Export(ctx)
	if err != nil {
		return err
	}
	if err := verifyKnowledgePromotion(snap, check); err != nil {
		return err
	}
	if _, err := remote.Context(ctx, ks.ContextRequest{Query: "workspace knowledge", ArtifactSelectors: []ks.ArtifactSelector{{Status: "active"}}, TokenBudget: 1000}); err != nil {
		return err
	}
	localPath := cfg.Knowledge.LocalPath
	if localPath == "" {
		localPath = filepath.Join(".vessica", "state", "knowledge.db")
	}
	if !filepath.IsAbs(localPath) {
		localPath = filepath.Join(app.Root, localPath)
	}
	backup := filepath.Join(app.Root, ".vessica", "state", "knowledge-"+snap.HighWatermark+".readonly.db")
	if err := copyReadOnly(localPath, backup); err != nil {
		return err
	}
	cfg.Knowledge.Mode = "hosted"
	return nil
}

func resolveGHCRDigest(ctx context.Context, image string) (string, error) {
	if strings.Contains(image, "@sha256:") {
		return image, nil
	}
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(image, prefix) {
		return "", fmt.Errorf("production image must be ghcr.io or already digest-pinned")
	}
	nameTag := strings.TrimPrefix(image, prefix)
	name, tag, ok := strings.Cut(nameTag, ":")
	if !ok {
		tag = "latest"
	}
	tokenURL := "https://ghcr.io/token?scope=repository:" + name + ":pull"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var authResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", err
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodHead, "https://ghcr.io/v2/"+name+"/manifests/"+tag, nil)
	req.Header.Set("Authorization", "Bearer "+authResp.Token)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry response omitted image digest")
	}
	return prefix + name + "@" + digest, nil
}

func configureRailwayService(ctx context.Context, root string, cfg config.Config, secrets railwaySecrets, linearToken, linearOAuth, railwayOAuth, githubToken, openAIKey, codexAuthB64 string) error {
	reference := "$" + "{{Postgres.DATABASE_URL}}"
	privateKey, err := os.ReadFile(railwaySSHPrivateKeyPath(root))
	if err != nil {
		return fmt.Errorf("read Railway SSH identity: %w", err)
	}
	variables := map[string]string{
		"DATABASE_URL": reference, "VES_DB_URL": reference, "VES_STATE_BACKEND": "postgres-url",
		"VES_SANDBOX": "railway", "VES_HOSTED_PROVIDER": "railway", "VES_CONTROL_PLANE_URL": cfg.Hosted.ControlPlaneURL,
		"VES_REPO_REMOTE": cfg.Repo.Remote, "VES_RUNNER": cfg.Runner.Default, "VES_RUNNER_MODEL": cfg.Runner.Model,
		"VES_RUNNER_REASONING_EFFORT": cfg.Runner.ReasoningEffort, "VES_TRACKER_PROVIDER": "linear",
		"VES_LINEAR_TEAM_ID": cfg.Tracker.TeamID, "VES_LINEAR_TODO_STATE_ID": cfg.Tracker.TodoStateID,
		"VES_LINEAR_WIP_STATE_ID": cfg.Tracker.WIPStateID, "VES_LINEAR_DONE_STATE_ID": cfg.Tracker.DoneStateID,
		"VES_LINEAR_BLOCKED_STATE_ID": cfg.Tracker.BlockedStateID, "VES_LINEAR_TRIGGER_LABEL": cfg.Tracker.TriggerLabel,
		"VES_RAILWAY_POSTGRES_SERVICE_ID": cfg.Hosted.PostgresServiceID,
		"VES_WORKER_DOWNLOAD_TOKEN":       secrets.WorkerToken, "VES_CONTROL_PLANE_API_TOKEN": secrets.APIToken,
		"VES_LINEAR_WEBHOOK_SECRET": secrets.WebhookSecret, "LINEAR_API_KEY": linearToken,
		"GITHUB_TOKEN": githubToken, "OPENAI_API_KEY": openAIKey, "RAILWAY_TOKEN": secrets.RuntimeToken,
		"VES_LINEAR_OAUTH_JSON": linearOAuth, "VES_RAILWAY_OAUTH_JSON": railwayOAuth,
		"VES_CREDENTIAL_ENCRYPTION_KEY": secrets.CredentialKey, "VES_CODEX_AUTH_B64": codexAuthB64,
		"VES_RAILWAY_SSH_PRIVATE_KEY": string(privateKey),
		"VES_KNOWLEDGE_MODE":          "hosted", "VES_KNOWLEDGE_WORKSPACE_ID": cfg.Knowledge.WorkspaceID,
		"VES_KNOWLEDGE_ENDPOINT": cfg.Knowledge.Endpoint, "VES_KNOWLEDGE_TOKEN": secrets.KnowledgeToken,
	}
	for key, value := range variables {
		if err := setRailwayVariable(ctx, cfg, key, value); err != nil {
			return err
		}
	}
	return nil
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

func objectString(raw []byte, wanted string) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("parse Railway JSON: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			if result, ok := x[wanted].(string); ok && result != "" {
				return result
			}
			for _, child := range x {
				if result := find(child); result != "" {
					return result
				}
			}
		case []any:
			for _, child := range x {
				if result := find(child); result != "" {
					return result
				}
			}
		}
		return ""
	}
	if result := find(value); result != "" {
		return result, nil
	}
	return "", fmt.Errorf("Railway response did not include %s", wanted)
}

func waitForHostedHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("control plane did not become healthy: %w", lastErr)
}

type railwayDeploymentStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func latestRailwayDeployment(ctx context.Context, cfg config.Config) (railwayDeploymentStatus, error) {
	return latestRailwayDeploymentForService(ctx, cfg, cfg.Hosted.ServiceID)
}

func latestRailwayDeploymentForService(ctx context.Context, cfg config.Config, serviceID string) (railwayDeploymentStatus, error) {
	raw, err := runRailway(ctx, "", nil, "deployment", "list", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", serviceID, "--limit", "1", "--json")
	if err != nil {
		return railwayDeploymentStatus{}, err
	}
	return parseLatestRailwayDeployment(raw)
}

func parseLatestRailwayDeployment(raw []byte) (railwayDeploymentStatus, error) {
	var deployments []railwayDeploymentStatus
	if err := json.Unmarshal(raw, &deployments); err != nil {
		return railwayDeploymentStatus{}, fmt.Errorf("parse Railway deployments: %w", err)
	}
	if len(deployments) == 0 || deployments[0].ID == "" {
		return railwayDeploymentStatus{}, fmt.Errorf("Railway did not return a deployment")
	}
	return deployments[0], nil
}

func waitForRailwayDeployment(ctx context.Context, cfg config.Config, previousID string, timeout time.Duration) error {
	return waitForRailwayDeploymentForService(ctx, cfg, cfg.Hosted.ServiceID, previousID, timeout)
}

func waitForRailwayDeploymentForService(ctx context.Context, cfg config.Config, serviceID, previousID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var latest railwayDeploymentStatus
	for time.Now().Before(deadline) {
		deployment, err := latestRailwayDeploymentForService(ctx, cfg, serviceID)
		if err == nil {
			latest = deployment
			if deployment.ID != previousID {
				switch strings.ToUpper(deployment.Status) {
				case "SUCCESS":
					return nil
				case "FAILED", "CRASHED", "REMOVED":
					return fmt.Errorf("Railway deployment %s finished with status %s; inspect `ves railway logs`", deployment.ID, deployment.Status)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("Railway deployment did not become successful within %s (latest id=%s status=%s)", timeout, latest.ID, latest.Status)
}

func hostedRequest(ctx context.Context, method, endpoint, token string, body any, target any) error {
	return hostedRequestWithKey(ctx, method, endpoint, token, "", body, target)
}

func hostedRequestWithKey(ctx context.Context, method, endpoint, token, idempotencyKey string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("hosted API failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target != nil {
		return json.Unmarshal(data, target)
	}
	return nil
}

func railwaySecretsPath(root string) string {
	return filepath.Join(root, config.DirName, "secrets", "railway.json")
}

func railwaySSHPrivateKeyPath(root string) string {
	return filepath.Join(root, config.DirName, "secrets", "railway_ed25519")
}

func railwaySSHUserKeyPath(cfg config.Config) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if len(cfg.Hosted.ProjectID) < 8 {
		return "", fmt.Errorf("invalid Railway project id")
	}
	return filepath.Join(home, ".ssh", "vessica_control_plane_"+cfg.Hosted.ProjectID[:8]), nil
}

func saveRailwaySecrets(root string, secrets railwaySecrets) error {
	if err := os.MkdirAll(filepath.Dir(railwaySecretsPath(root)), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(secrets, "", "  ")
	return os.WriteFile(railwaySecretsPath(root), data, 0o600)
}

func loadRailwaySecrets(root string) (railwaySecrets, error) {
	var secrets railwaySecrets
	data, err := os.ReadFile(railwaySecretsPath(root))
	if err != nil {
		return secrets, err
	}
	return secrets, json.Unmarshal(data, &secrets)
}

func randomSecret(bytesCount int) string {
	return hex.EncodeToString(randomBytes(bytesCount))
}

func randomBytes(bytesCount int) []byte {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func moduleIsVessica(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	return err == nil && strings.Contains(string(data), "module github.com/vessica-labs/vessica-cli")
}
