package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/controlplane"
	"github.com/vessica-labs/vessica-cli/internal/dashboard"
	"github.com/vessica-labs/vessica-cli/internal/pack"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func newControlPlaneCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "control-plane", Short: "Run Vessica hosted control-plane roles", Hidden: true}
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Apply hosted database migrations before starting a deployment",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.TeamDefaults()
			config.ApplyEnv(&cfg)
			if cfg.State.DBURL == "" {
				cfg.State.DBURL = os.Getenv("VES_CONTROL_DATABASE_URL")
			}
			if cfg.State.DBURL == "" {
				return fmt.Errorf("VES_CONTROL_DATABASE_URL is required")
			}
			db, err := state.OpenWithOptions("postgres-url", cfg.State.DBURL, "/var/lib/vessica", state.OpenOptions{})
			if err != nil {
				return err
			}
			defer db.Close()
			return db.Migrate(cmd.Context())
		},
	}
	cmd.AddCommand(migrate)
	cmd.AddCommand(newControlPlaneRailwaySessionCmd())
	var previewEdgeAddr string
	previewEdge := &cobra.Command{
		Use:   "preview-edge",
		Short: "Serve the isolated public preview origin",
		RunE: func(cmd *cobra.Command, args []string) error {
			return controlplane.RunPreviewEdge(
				cmd.Context(), previewEdgeAddr,
				os.Getenv("VES_PREVIEW_UPSTREAM"),
				os.Getenv("VES_PREVIEW_EDGE_TOKEN"),
			)
		},
	}
	previewEdge.Flags().StringVar(&previewEdgeAddr, "addr", envDefault("PORT", "8080"), "listen address or port")
	previewEdge.PreRunE = func(cmd *cobra.Command, args []string) error {
		if !strings.HasPrefix(previewEdgeAddr, ":") && !strings.Contains(previewEdgeAddr, ":") {
			previewEdgeAddr = ":" + previewEdgeAddr
		}
		return nil
	}
	cmd.AddCommand(previewEdge)
	var addr string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve webhooks, jobs, previews, and the hosted API",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.TeamDefaults()
			config.ApplyEnv(&cfg)
			if cfg.State.DBURL == "" {
				cfg.State.DBURL = os.Getenv("VES_CONTROL_DATABASE_URL")
			}
			if cfg.State.DBURL == "" {
				return fmt.Errorf("VES_CONTROL_DATABASE_URL is required")
			}
			if err := configureHostedAuth(false); err != nil {
				return err
			}
			root := envDefault("VES_CONTROL_PLANE_ROOT", "/var/lib/vessica")
			if err := os.MkdirAll(root, 0o755); err != nil {
				return err
			}
			db, err := state.OpenWithOptions("postgres-url", cfg.State.DBURL, root, state.OpenOptions{})
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.VerifySchema(cmd.Context()); err != nil {
				return err
			}
			workspaceKey := hostedWorkspaceKey(cfg)
			workspaceID := firstNonEmpty(os.Getenv("VES_WORKSPACE_ID"), os.Getenv("VES_KNOWLEDGE_WORKSPACE_ID"))
			if _, err := db.EnsureHostedWorkspaceWithID(cmd.Context(), workspaceID, workspaceKey); err != nil {
				return err
			}
			credentialManager, err := hostedCredentialManager(cmd.Context(), db)
			if err != nil {
				return err
			}
			var railwaySession *controlplane.RailwayCLISession
			if credentialManager != nil {
				railwaySession = controlplane.NewRailwayCLISession(
					credentialManager, railwayPath(), filepath.Join(root, ".vessica", "railway-cli"),
					firstNonEmpty(os.Getenv("VES_RAILWAY_WORKSPACE_ID"), cfg.Hosted.WorkspaceID),
				)
				if _, restoreErr := railwaySession.Restore(cmd.Context()); restoreErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Railway CLI session restore failed: %v\n", restoreErr)
				}
			}
			linearToken := strings.TrimSpace(os.Getenv("LINEAR_API_KEY"))
			var linear *tracker.LinearClient
			if linearToken != "" {
				linear = tracker.NewLinearClient(linearToken)
			} else if credentialManager != nil {
				linear = tracker.NewLinearClientWithTokenSource(func(ctx context.Context) (string, error) {
					return credentialManager.Token(ctx, "linear")
				})
			}
			if linear != nil && cfg.Tracker.Provider == "linear" {
				if _, err := db.UpsertTrackerIntegration(cmd.Context(), "linear", "connected", cfg.Tracker, os.Getenv("VES_LINEAR_WEBHOOK_ID"), "oauth:linear"); err != nil {
					return err
				}
			}
			broker := controlplane.NewPreviewBroker()
			launcher := &controlplane.RailwayLauncher{
				DB: db, Config: cfg, CLIPath: railwayPath(), PublicURL: cfg.Hosted.ControlPlaneURL,
				PreviewPublicURL:    os.Getenv("VES_PREVIEW_ORIGIN"),
				WorkerDownloadToken: os.Getenv("VES_WORKER_DOWNLOAD_TOKEN"), Broker: broker,
				RailwaySession: railwaySession,
			}
			if credentialManager != nil && credentialManager.Has(cmd.Context(), "railway") {
				launcher.RailwayToken = func(ctx context.Context) (string, error) {
					return credentialManager.Token(ctx, "railway")
				}
			}
			server := &controlplane.Server{
				DB: db, Config: cfg, Linear: linear, Launcher: launcher, PreviewBroker: broker,
				Credentials:         credentialManager,
				PreviewEdgeToken:    os.Getenv("VES_PREVIEW_EDGE_TOKEN"),
				LinearWebhookSecret: os.Getenv("VES_LINEAR_WEBHOOK_SECRET"),
				APIToken:            os.Getenv("VES_CONTROL_PLANE_API_TOKEN"),
				AgentRuntimeToken:   os.Getenv("VES_AGENT_RUNTIME_TOKEN"),
				WorkerDownloadToken: os.Getenv("VES_WORKER_DOWNLOAD_TOKEN"),
				Logger:              log.New(os.Stdout, "control-plane ", log.LstdFlags|log.LUTC),
				PreviewPublicURL:    os.Getenv("VES_PREVIEW_ORIGIN"),
			}
			if !strings.EqualFold(strings.TrimSpace(os.Getenv("VES_DASHBOARD_ENABLED")), "false") {
				dash := dashboard.New(appservice.New(db, root, cfg), "hosted")
				dash.Origin = firstNonEmpty(os.Getenv("VES_DASHBOARD_ORIGIN"), cfg.Hosted.ControlPlaneURL)
				dash.PreviewOrigin = os.Getenv("VES_PREVIEW_ORIGIN")
				dash.ServiceToken = os.Getenv("VES_CONTROL_PLANE_API_TOKEN")
				dash.GitHubClientID = firstNonEmpty(os.Getenv("VES_GITHUB_OAUTH_CLIENT_ID"), dashboard.DefaultGitHubClientID)
				dash.PreviewAccess = func(ctx context.Context, runID string) (string, error) {
					capability, err := broker.Issue(runID, 15*time.Minute)
					if err != nil {
						return "", err
					}
					base := strings.TrimRight(os.Getenv("VES_PREVIEW_ORIGIN"), "/")
					if base == "" {
						return "", fmt.Errorf("VES_PREVIEW_ORIGIN is required")
					}
					return base + "/previews/" + url.PathEscape(runID) + "/?cap=" + url.QueryEscape(capability), nil
				}
				dash.RefineAction = server.DashboardRefine
				dash.ApproveAction = server.DashboardApprove
				dash.RollbackAction = server.DashboardRollback
				dash.CancelAction = server.DashboardCancel
				dash.RetainAction = server.DashboardRetain
				dash.DestroyAction = server.DashboardDestroy
				dash.RuntimeStatus = server.AgentRuntimeStatus
				server.Dashboard = dash.Handler()
			}
			go server.RestoreHostedPreviews(cmd.Context())
			return server.Run(cmd.Context(), addr)
		},
	}
	serve.Flags().StringVar(&addr, "addr", envDefault("PORT", "8080"), "listen address or port")
	serve.PreRunE = func(cmd *cobra.Command, args []string) error {
		if !strings.HasPrefix(addr, ":") && !strings.Contains(addr, ":") {
			addr = ":" + addr
		}
		return nil
	}
	cmd.AddCommand(serve)

	var runID, fromPhase string
	worker := &cobra.Command{
		Use:   "worker",
		Short: "Execute one run inside a Railway sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runID == "" {
				return fmt.Errorf("--run-id is required")
			}
			engine, db, err := openHostedWorker(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			result, err := engine.Resume(cmd.Context(), runID, fromPhase)
			if result != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "run %s: %s\n", result.ID, result.Status)
			}
			return err
		},
	}
	worker.Flags().StringVar(&runID, "run-id", "", "run id to execute")
	worker.Flags().StringVar(&fromPhase, "from", "", "phase to resume from")

	var promptRunID, promptB64 string
	promptWorker := &cobra.Command{
		Use:   "prompt-worker",
		Short: "Apply one direct prompt inside a Railway sandbox",
		RunE: func(cmd *cobra.Command, args []string) error {
			if promptRunID == "" || promptB64 == "" {
				return fmt.Errorf("--run-id and --prompt-b64 are required")
			}
			prompt, err := base64.RawURLEncoding.DecodeString(promptB64)
			if err != nil {
				return fmt.Errorf("decode prompt: %w", err)
			}
			engine, db, err := openHostedWorker(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()
			sandboxRecord, err := db.GetSandboxForRun(cmd.Context(), promptRunID)
			if err != nil {
				return err
			}
			result, err := engine.PromptSandbox(cmd.Context(), sandboxRecord.ID, runengine.PromptOptions{Prompt: string(prompt), Push: true})
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(result)
			fmt.Fprintf(cmd.OutOrStdout(), "VES_PROMPT_RESULT:%s\n", encoded)
			return nil
		},
	}
	promptWorker.Flags().StringVar(&promptRunID, "run-id", "", "run id attached to the retained sandbox")
	promptWorker.Flags().StringVar(&promptB64, "prompt-b64", "", "base64url-encoded refinement prompt")
	cmd.AddCommand(worker, promptWorker)
	return cmd
}

func openHostedWorker(ctx context.Context) (*runengine.Engine, *state.DB, error) {
	workerStarted := time.Now()
	timings := []map[string]any{}
	stage := func(name string, started time.Time, detail map[string]any) {
		item := map[string]any{"stage": name, "duration_ms": time.Since(started).Milliseconds(), "status": "completed"}
		for key, value := range detail {
			item[key] = value
		}
		timings = append(timings, item)
	}
	cfg := config.TeamDefaults()
	config.ApplyEnv(&cfg)
	cfg.State.Backend = "postgres-url"
	cfg.Sandbox.Backend = "railway"
	if cfg.State.DBURL == "" {
		cfg.State.DBURL = os.Getenv("VES_CONTROL_DATABASE_URL")
	}
	authStarted := time.Now()
	if err := configureHostedAuth(true); err != nil {
		return nil, nil, err
	}
	if err := installCodexAuth(); err != nil {
		return nil, nil, err
	}
	stage("worker_auth_setup", authStarted, nil)
	root := filepath.Join("/workspace", "repo")
	repoStarted := time.Now()
	repoResult, err := ensureWorkerRepo(ctx, root, cfg.Repo.Remote)
	if err != nil {
		return nil, nil, err
	}
	stage("repository_sync", repoStarted, repoResult)
	configStarted := time.Now()
	if err := config.Save(root, cfg); err != nil {
		return nil, nil, err
	}
	stage("worker_config", configStarted, nil)
	packStarted := time.Now()
	if _, err := pack.EnsureHosted(root); err != nil {
		return nil, nil, err
	}
	stage("engineering_pack", packStarted, map[string]any{"cache_hit": true})
	databaseStarted := time.Now()
	db, err := state.OpenWithOptions(cfg.State.Backend, cfg.State.DBURL, root, state.OpenOptions{})
	if err != nil {
		return nil, nil, err
	}
	if err := db.VerifySchema(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	if _, err := db.GetWorkspace(ctx); err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	stage("worker_database", databaseStarted, nil)
	stage("worker_binary_ready", workerStarted, nil)
	recordBootstrapTimings(ctx, db, os.Getenv("VES_RUN_ID"), timings)
	for _, key := range []string{
		"VES_CONTROL_DATABASE_URL", "VES_KNOWLEDGE_DATABASE_URL", "GITHUB_TOKEN", "LINEAR_API_KEY",
		"RAILWAY_TOKEN", "RAILWAY_API_TOKEN", "VES_WORKER_DOWNLOAD_TOKEN",
		"VES_CONTROL_PLANE_API_TOKEN",
		"VES_CODEX_AUTH_B64",
	} {
		_ = os.Unsetenv(key)
	}
	return &runengine.Engine{DB: db, Root: root, Config: cfg, Local: true, Stream: true}, db, nil
}

func hostedCredentialManager(ctx context.Context, db *state.DB) (*controlplane.CredentialManager, error) {
	initial := map[string]string{
		"railway": strings.TrimSpace(os.Getenv("VES_RAILWAY_OAUTH_JSON")),
		"linear":  strings.TrimSpace(os.Getenv("VES_LINEAR_OAUTH_JSON")),
	}
	if initial["railway"] == "" && initial["linear"] == "" && strings.TrimSpace(os.Getenv("VES_CREDENTIAL_ENCRYPTION_KEY")) == "" {
		return nil, nil
	}
	return controlplane.NewCredentialManagerWithValidator(ctx, db, os.Getenv("VES_CREDENTIAL_ENCRYPTION_KEY"), initial, validateHostedCredential)
}

func validateHostedCredential(ctx context.Context, provider, accessToken string) error {
	if strings.TrimSpace(accessToken) == "" {
		return fmt.Errorf("access token is empty")
	}
	switch provider {
	case "linear":
		_, err := tracker.NewLinearClient(accessToken).Discover(ctx)
		return err
	case "railway":
		command := exec.CommandContext(ctx, railwayPath(), "whoami", "--json")
		command.Env = append(os.Environ(), "RAILWAY_API_TOKEN="+accessToken)
		if err := command.Run(); err != nil {
			return fmt.Errorf("Railway rejected the supplied OAuth credential")
		}
		return nil
	default:
		return fmt.Errorf("unsupported OAuth provider %q", provider)
	}
}

func installCodexAuth() error {
	raw := strings.TrimSpace(os.Getenv("VES_CODEX_AUTH_B64"))
	if raw == "" {
		return nil
	}
	data, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return fmt.Errorf("decode Codex login: %w", err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("Codex login is not valid JSON")
	}
	home := strings.TrimSpace(os.Getenv("VES_RUNNER_HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if runner := strings.TrimSpace(os.Getenv("VES_RUNNER_USER")); runner != "" {
		account, err := user.Lookup(runner)
		if err != nil {
			return fmt.Errorf("resolve runner account: %w", err)
		}
		uid, uidErr := strconv.Atoi(account.Uid)
		gid, gidErr := strconv.Atoi(account.Gid)
		if uidErr != nil || gidErr != nil {
			return fmt.Errorf("resolve runner ownership")
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return err
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
	}
	return os.Chmod(path, 0o600)
}

func configureHostedAuth(required bool) error {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		if required {
			return fmt.Errorf("GITHUB_TOKEN is required")
		}
		return nil
	}
	return auth.Login("github", token, "hosted")
}

func hostedWorkspaceKey(cfg config.Config) string {
	return "hosted://" + cfg.Hosted.ProjectID
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
