package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

type railwaySecrets struct {
	RuntimeToken, ServiceToken, APIToken, WorkerToken, PreviewEdgeToken, WebhookSecret, WebhookID, CredentialKey, KnowledgeToken, KnowledgeAdminToken string
	ControlDatabasePassword, KnowledgeDatabasePassword                                                                                                string
	AgentRuntimeToken                                                                                                                                 string
}

type railwayUpOptions struct {
	Workspace, WorkspaceName, Source, Image, RuntimeToken, LinearToken, GitHubToken, OpenAIKey, PreviewOrigin string
	Team, LinearProject, TodoState, WIPState, DoneState, BlockedState, TriggerLabel, WorkerCheckpoint         string
	KnowledgeImage, KnowledgeSource, EmbeddingAPIKey, EmbeddingAPIKeyEnv                                      string
	AgentRuntimeImage, AgentRuntimeSource                                                                     string
	EnableLinear                                                                                              bool
	Progress                                                                                                  func(string)
}

func defaultControlPlaneImage() string {
	return "ghcr.io/vessica-labs/vessica-cli:" + version.Version
}

func newRailwayCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "railway", Short: "Provision and operate the hosted Railway control plane"}
	var opts railwayUpOptions
	up := &cobra.Command{
		Use: "up", Short: "Provision the hosted control plane, lexical knowledge, and Railway sandbox access", Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
				return app.Printer.Fail("not_initialized", err.Error(), "run ves init first")
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("railway.up", map[string]any{
					"project_name":            railwayControlPlaneProjectName,
					"workspace":               opts.Workspace,
					"control_plane_source":    opts.Source,
					"control_plane_image":     opts.Image,
					"knowledge_image":         opts.KnowledgeImage,
					"knowledge_source":        opts.KnowledgeSource,
					"embedding_key_env":       opts.EmbeddingAPIKeyEnv,
					"linear_team":             opts.Team,
					"linear_project":          opts.LinearProject,
					"worker_checkpoint":       firstNonEmpty(opts.WorkerCheckpoint, "vessica-worker-toolchain-"+toolchain.Fingerprint()),
					"preserves_local_on_fail": true,
				})
			}
			if err := app.requireYes("provision Railway infrastructure and promote knowledge"); err != nil {
				return err
			}
			result, err := railwayUp(cmd.Context(), app, opts)
			if err != nil {
				return app.Printer.Fail("railway_up_failed", err.Error(), "run ves railway status and ves railway logs for details")
			}
			return app.Printer.Success(result)
		},
	}
	up.Flags().StringVar(&opts.Workspace, "workspace", "", "Railway workspace id or name")
	up.Flags().StringVar(&opts.Source, "source", "", "deploy control-plane source from this directory")
	up.Flags().StringVar(&opts.Image, "image", defaultControlPlaneImage(), "published control-plane image")
	up.Flags().StringVar(&opts.RuntimeToken, "railway-token", "", "headless fallback project token for the hosted service")
	up.Flags().StringVar(&opts.LinearToken, "linear-token", "", "headless fallback Linear API key")
	up.Flags().StringVar(&opts.GitHubToken, "github-token", "", "GitHub token with repository and PR access")
	up.Flags().StringVar(&opts.OpenAIKey, "openai-api-key", "", "headless fallback OpenAI API key for hosted Codex")
	up.Flags().StringVar(&opts.Team, "linear-team", "", "Linear team id, key, or name")
	up.Flags().StringVar(&opts.LinearProject, "linear-project", "", "default Linear project id, slug, or name")
	up.Flags().StringVar(&opts.TodoState, "todo-state", "Todo", "Linear Todo state id or name")
	up.Flags().StringVar(&opts.WIPState, "wip-state", "In Progress", "Linear WIP state id or name")
	up.Flags().StringVar(&opts.DoneState, "done-state", "Done", "Linear Done state id or name")
	up.Flags().StringVar(&opts.BlockedState, "blocked-state", "", "optional Linear blocked state id or name")
	up.Flags().StringVar(&opts.TriggerLabel, "trigger-label", "", "only process issues with this label")
	up.Flags().StringVar(&opts.WorkerCheckpoint, "worker-checkpoint", "", "override the managed Railway sandbox toolchain checkpoint")
	up.Flags().StringVar(&opts.PreviewOrigin, "preview-origin", "", "separate HTTPS origin for hosted previews")
	up.Flags().StringVar(&opts.KnowledgeImage, "knowledge-image", "", "knowledge-server OCI image override (resolved to an immutable digest)")
	up.Flags().StringVar(&opts.KnowledgeSource, "knowledge-source", "", "development-only knowledge-server source directory")
	up.Flags().StringVar(&opts.AgentRuntimeImage, "agent-runtime-image", "", "agent-runtime OCI image override (resolved to an immutable digest)")
	up.Flags().StringVar(&opts.AgentRuntimeSource, "agent-runtime-source", "", "development-only agent-runtime source directory")
	up.Flags().StringVar(&opts.EmbeddingAPIKeyEnv, "embedding-api-key-env", "", "optional environment variable containing an embedding provider key")
	cmd.AddCommand(up, newRailwayStatusCmd(app), newRailwayLogsCmd(app), newRailwayApproveCmd(app), newRailwayPreviewSessionCmd(app), newRailwayDownCmd(app))
	return cmd
}

func newRailwayStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show hosted control-plane and deployment status", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
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
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
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
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
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
		if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
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
		_ = auth.DeleteSecret(railwaySecretsReference(app.Root))
		return app.Printer.Success(map[string]any{"deleted": true, "project_id": projectID})
	}}
}
