package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

func railwayUp(ctx context.Context, app *App, opts railwayUpOptions) (map[string]any, error) {
	progress := func(message string) {
		if opts.Progress != nil {
			opts.Progress(message)
		}
	}
	linearOAuth, linearToken := "", ""
	if opts.EnableLinear || opts.LinearToken != "" || opts.Team != "" {
		linearOAuth, _ = auth.MarshalOAuth("linear")
		linearToken = firstNonEmpty(opts.LinearToken, os.Getenv("LINEAR_API_KEY"))
		if linearToken == "" {
			linearToken, _ = auth.Token("linear")
		}
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
	secrets, err := loadOptionalRailwaySecrets(app.Root)
	if err != nil {
		return nil, fmt.Errorf("load retained Railway credentials: %w", err)
	}
	runtimeToken := firstNonEmpty(opts.RuntimeToken, os.Getenv("RAILWAY_TOKEN"), secrets.RuntimeToken)
	embeddingKey := opts.EmbeddingAPIKey
	if embeddingKey == "" && opts.EmbeddingAPIKeyEnv != "" {
		embeddingKey = os.Getenv(opts.EmbeddingAPIKeyEnv)
	}
	var missing []string
	if runtimeToken == "" && railwayOAuth == "" {
		missing = append(missing, "Railway browser login")
	}
	if githubToken == "" {
		missing = append(missing, "GitHub token")
	}
	if openAIKey == "" && codexAuthB64 == "" {
		missing = append(missing, "Codex browser login")
	}
	if len(missing) > 0 {
		return map[string]any{
			"status":  "awaiting_credentials",
			"missing": missing,
			"next":    "Authenticate the listed providers, then rerun ves up.",
		}, nil
	}
	secrets = initializeRailwaySecrets(secrets, runtimeToken)
	if err := saveRailwaySecrets(app.Root, secrets); err != nil {
		return nil, err
	}
	if opts.Source == "" {
		image := firstNonEmpty(opts.Image, defaultControlPlaneImage())
		resolved, resolveErr := resolveGHCRDigest(ctx, image)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve control-plane release image: %w", resolveErr)
		}
		opts.Image = resolved
	}
	cfg := app.Config
	cfg.Hosted.Provider, cfg.Hosted.WorkerCheckpoint = "railway", opts.WorkerCheckpoint
	workDir, err := os.MkdirTemp("", "ves-railway-up-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	if cfg.Hosted.ProjectID == "" || cfg.Hosted.ServiceID == "" || cfg.Hosted.PostgresServiceID == "" {
		progress("creating or reconciling the Railway project, control-plane service, and Postgres")
		if err := createRailwayResources(ctx, workDir, app.Root, &cfg, opts); err != nil {
			return nil, err
		}
		app.Config = cfg
	}
	progress("Railway project, control-plane service, and Postgres are present")
	if err := reconcileRailwayResourceIDs(ctx, &cfg); err != nil {
		return nil, err
	}
	if err := linkRailwayWorkDir(ctx, workDir, cfg); err != nil {
		return nil, err
	}
	if cfg.Hosted.WorkerCheckpoint == "" {
		progress("building and verifying the managed Railway worker checkpoint")
		checkpoint, err := ensureRailwayWorkerCheckpoint(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfg.Hosted.WorkerCheckpoint = checkpoint
	}
	progress("managed Railway worker checkpoint is ready")
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	app.Config = cfg
	progress("initializing isolated control and knowledge databases")
	databaseURLs, err := ensureRailwayDatabases(ctx, cfg, secrets)
	if err != nil {
		return nil, err
	}
	progress("control and knowledge databases are initialized")
	var linear *tracker.LinearClient
	var team tracker.LinearTeam
	if linearToken != "" || linearOAuth != "" {
		if linearToken == "" {
			linearToken, err = auth.Token("linear")
			if err != nil {
				return nil, err
			}
		}
		linear = tracker.NewLinearClient(linearToken)
		discovery, discoverErr := linear.Discover(ctx)
		if discoverErr != nil {
			return nil, fmt.Errorf("discover Linear workspace: %w", discoverErr)
		}
		team, states, resolveErr := resolveLinearConfig(discovery, opts)
		if resolveErr != nil {
			return nil, resolveErr
		}
		cfg.Tracker.Provider = "linear"
		cfg.Tracker.TeamID, cfg.Tracker.TodoStateID = team.ID, states["todo"]
		project, resolveErr := resolveLinearProject(discovery, team.ID, opts.LinearProject, cfg.Tracker.ProjectID)
		if resolveErr != nil {
			return nil, resolveErr
		}
		cfg.Tracker.ProjectID = project.ID
		cfg.Tracker.WIPStateID, cfg.Tracker.DoneStateID = states["wip"], states["done"]
		cfg.Tracker.BlockedStateID = states["blocked"]
		cfg.Tracker.TriggerLabel = resolvedTriggerLabel(opts.TriggerLabel, cfg.Tracker.TriggerLabel)
		if cfg.Tracker.TriggerLabel != "" {
			if _, err := linear.EnsureIssueLabel(ctx, team.ID, cfg.Tracker.TriggerLabel); err != nil {
				return nil, fmt.Errorf("ensure Linear trigger label: %w", err)
			}
		}
	} else {
		cfg.Tracker = config.TrackerConfig{Provider: "none"}
	}
	if err := ensureRailwayDomain(ctx, workDir, &cfg); err != nil {
		return nil, err
	}
	if opts.PreviewOrigin != "" {
		if err := ensureRailwayPreviewDomain(ctx, workDir, cfg, opts.PreviewOrigin); err != nil {
			return nil, err
		}
		cfg.Hosted.PreviewServiceID = ""
		cfg.Hosted.PreviewURL = strings.TrimRight(opts.PreviewOrigin, "/")
	} else {
		progress("provisioning the isolated Railway preview origin")
		if err := ensureRailwayPreviewEdge(ctx, workDir, &cfg, opts, secrets); err != nil {
			return nil, err
		}
		progress("isolated Railway preview origin is ready")
	}
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	progress("deploying and verifying the hosted knowledge service")
	if err := ensureRailwayKnowledge(ctx, workDir, app, &cfg, opts, databaseURLs.Knowledge, secrets.KnowledgeToken, secrets.KnowledgeAdminToken, embeddingKey); err != nil {
		return nil, err
	}
	progress("hosted knowledge service is ready")
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	hostedLinearToken := linearToken
	if linearOAuth != "" {
		hostedLinearToken = ""
	}
	progress("configuring control-plane credentials and service variables")
	if err := configureRailwayService(ctx, cfg, secrets, databaseURLs.Control, hostedLinearToken, linearOAuth, railwayOAuth, githubToken, openAIKey, codexAuthB64, cfg.Hosted.PreviewURL); err != nil {
		return nil, err
	}
	progress("configuring the control-plane migration and image source")
	previousDeploymentID := ""
	if latest, err := latestRailwayDeployment(ctx, cfg); err == nil {
		previousDeploymentID = latest.ID
	}
	if err := configureRailwayControlPlaneMigration(ctx, cfg); err != nil {
		return nil, err
	}
	source := opts.Source
	if source != "" {
		if _, err := runRailway(ctx, source, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--detach", "--json", "--message", "Vessica "+version.Version); err != nil {
			return nil, err
		}
	} else if cfg.Hosted.ControlPlaneImage != opts.Image {
		// Attaching the image is the first action that can deploy a freshly
		// created service. The migration command and database variables are now
		// already present, so the first process start is safe.
		if err := configureRailwayControlPlaneImage(ctx, cfg, opts.Image); err != nil {
			return nil, err
		}
		cfg.Hosted.ControlPlaneImage = opts.Image
		if err := config.Save(app.Root, cfg); err != nil {
			return nil, err
		}
		app.Config = cfg
	} else {
		if _, err := runRailway(ctx, "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.ServiceID, "--yes"); err != nil {
			return nil, err
		}
	}
	if err := waitForRailwayDeployment(ctx, cfg, previousDeploymentID, 8*time.Minute); err != nil {
		return nil, err
	}
	progress("control-plane deployment reached success; verifying readiness")
	if err := waitForHostedHealth(ctx, cfg.Hosted.ControlPlaneURL+"/readyz", 8*time.Minute); err != nil {
		return nil, err
	}
	progress("control plane is ready")
	if secrets.APIToken == "" {
		subject, validateErr := auth.ValidateGitHubToken(githubToken)
		if validateErr != nil {
			return nil, fmt.Errorf("resolve GitHub identity for CLI credential: %w", validateErr)
		}
		var credential struct {
			Token string `json:"token"`
		}
		if err := hostedRequest(ctx, "POST", strings.TrimRight(cfg.Hosted.ControlPlaneURL, "/")+"/api/v1/cli-credentials", secrets.ServiceToken, map[string]string{"subject": subject}, &credential); err != nil {
			return nil, fmt.Errorf("issue user-scoped CLI credential: %w", err)
		}
		if credential.Token == "" {
			return nil, fmt.Errorf("control plane returned an empty CLI credential")
		}
		secrets.APIToken = credential.Token
		if err := saveRailwaySecrets(app.Root, secrets); err != nil {
			return nil, err
		}
	}
	if linear != nil && secrets.WebhookID == "" {
		webhook, err := linear.CreateWebhook(ctx, team.ID, cfg.Hosted.ControlPlaneURL+"/webhooks/linear", secrets.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("create Linear webhook: %w", err)
		}
		secrets.WebhookID = webhook.ID
		if err := saveRailwaySecrets(app.Root, secrets); err != nil {
			return nil, err
		}
	}
	if linear != nil && secrets.WebhookID != "" {
		if err := setRailwayVariable(ctx, cfg, "VES_LINEAR_WEBHOOK_ID", secrets.WebhookID); err != nil {
			return nil, err
		}
	}
	if err := config.Save(app.Root, cfg); err != nil {
		return nil, err
	}
	app.Config = cfg
	return map[string]any{"status": "running", "project_id": cfg.Hosted.ProjectID, "environment_id": cfg.Hosted.EnvironmentID,
		"service_id": cfg.Hosted.ServiceID, "postgres_service_id": cfg.Hosted.PostgresServiceID,
		"preview_service_id": cfg.Hosted.PreviewServiceID, "control_plane_url": cfg.Hosted.ControlPlaneURL,
		"preview_url": cfg.Hosted.PreviewURL, "webhook_id": secrets.WebhookID,
		"knowledge_endpoint": cfg.Knowledge.Endpoint, "knowledge_service_id": cfg.Knowledge.ServiceID,
		"retrieval_mode": "lexical", "embedding_state": "not_configured",
		"linear_connected": linear != nil, "linear_team": team.Name, "linear_project_id": cfg.Tracker.ProjectID, "todo_state_id": cfg.Tracker.TodoStateID}, nil
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

func ensureRailwaySSHIdentity(ctx context.Context, cfg config.Config) error {
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
	name := "vessica-worker-toolchain-" + toolchain.Fingerprint()
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
	return toolchain.CheckpointInstallCommand()
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

const knowledgeServerVersion = "0.4.0"
