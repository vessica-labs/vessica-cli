package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

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
	embeddingKey := opts.EmbeddingAPIKey
	if embeddingKey == "" && opts.EmbeddingAPIKeyEnv != "" {
		embeddingKey = os.Getenv(opts.EmbeddingAPIKeyEnv)
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
	if embeddingKey == "" {
		missing = append(missing, "embedding provider API key")
	}
	if len(missing) > 0 {
		return map[string]any{
			"status":  "awaiting_credentials",
			"missing": missing,
			"next":    "Authenticate the listed providers, set the embeddings credential in the environment named by --embedding-api-key-env, then rerun ves railway up.",
		}, nil
	}
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
	if err := linkRailwayWorkDir(ctx, workDir, cfg); err != nil {
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
	cfg.Tracker.BlockedStateID = states["blocked"]
	cfg.Tracker.TriggerLabel = resolvedTriggerLabel(opts.TriggerLabel, cfg.Tracker.TriggerLabel)
	if cfg.Tracker.TriggerLabel != "" {
		if _, err := linear.EnsureIssueLabel(ctx, team.ID, cfg.Tracker.TriggerLabel); err != nil {
			return nil, fmt.Errorf("ensure Linear trigger label: %w", err)
		}
	}
	if err := ensureRailwayDomain(ctx, workDir, &cfg); err != nil {
		return nil, err
	}
	if opts.PreviewOrigin != "" {
		if err := ensureRailwayPreviewDomain(ctx, workDir, cfg, opts.PreviewOrigin); err != nil {
			return nil, err
		}
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
	if err := configureRailwayService(ctx, app.Root, cfg, secrets, hostedLinearToken, linearOAuth, railwayOAuth, githubToken, openAIKey, codexAuthB64, opts.PreviewOrigin); err != nil {
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

func linkRailwayWorkDir(ctx context.Context, workDir string, cfg config.Config) error {
	if cfg.Hosted.ProjectID == "" || cfg.Hosted.EnvironmentID == "" {
		return fmt.Errorf("Railway project and environment ids are required before linking the provisioning workspace")
	}
	if _, err := runRailway(ctx, workDir, nil, "link", "--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID, "--json"); err != nil {
		return fmt.Errorf("link Railway provisioning workspace: %w", err)
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
	name := "vessica-worker-" + strings.ReplaceAll(version.Version, ".", "-") + "-toolchain-" + toolchain.CheckpointVersion
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cleanupCancel()
		_, _ = runRailway(cleanupCtx, "", nil, append(base, "destroy", "--id", sandboxID)...)
	}()
	install := railwayCheckpointInstallCommand()
	execArgs := append(base, "exec", "--id", sandboxID, "--timeout", "1200", "--", "bash", "-lc", install)
	if _, err := runRailway(ctx, "", nil, execArgs...); err != nil {
		return "", err
	}
	if _, err := runRailway(ctx, "", nil, append(base, "checkpoint", "create", name, "--id", sandboxID, "--json")...); err != nil {
		return "", err
	}
	return name, nil
}

func railwayCheckpointInstallCommand() string {
	return "set -e; " +
		"apt-get update && apt-get install -y --no-install-recommends util-linux && rm -rf /var/lib/apt/lists/*; " +
		"id -u vessica-agent >/dev/null 2>&1 || useradd --create-home --shell /bin/bash vessica-agent; " +
		"npm install -g pnpm@" + toolchain.PNPMVersion + " @openai/codex@" + toolchain.CodexVersion + " playwright@" + toolchain.PlaywrightVersion + "; " +
		"export NODE_PATH=$(npm root -g); " +
		"playwright install --with-deps chromium; " +
		"node -e 'const {chromium}=require(\"playwright\"); (async()=>{const b=await chromium.launch({headless:true}); await b.close()})().catch(e=>{console.error(e);process.exit(1)})'"
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

func ensureRailwayPreviewDomain(ctx context.Context, workDir string, cfg config.Config, origin string) error {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return fmt.Errorf("preview origin must be an HTTPS URL")
	}
	host := parsed.Hostname()
	raw, listErr := runRailway(ctx, workDir, nil, "domain", "list", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--json")
	if listErr == nil && strings.Contains(string(raw), host) {
		return nil
	}
	_, err = runRailway(ctx, workDir, nil, "domain", host, "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "-p", "8080", "--json")
	return err
}

const knowledgeServerVersion = "0.3.1"
