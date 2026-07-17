package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
)

func newWorkspaceCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Short: "Inspect the hosted Vessica workspace"}
	cmd.AddCommand(&cobra.Command{Use: "status", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Hosted.ControlPlaneURL != "" {
			secrets, err := loadRailwaySecrets(app.Root)
			if err != nil {
				return err
			}
			var hosted any
			endpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/status"
			if err := hostedRequest(c.Context(), http.MethodGet, endpoint, secrets.APIToken, nil, &hosted); err != nil {
				return err
			}
			return app.Printer.Success(hosted)
		}
		ws, err := app.DB.GetWorkspace(c.Context())
		if err != nil {
			return err
		}
		repositories, err := app.DB.ListRepositories(c.Context())
		if err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"workspace_id": ws.ID, "provider": app.Config.Hosted.Provider, "project_id": app.Config.Hosted.ProjectID, "endpoint": app.Config.Hosted.ControlPlaneURL, "knowledge": map[string]any{"mode": app.Config.Knowledge.Mode, "endpoint": app.Config.Knowledge.Endpoint, "embedding_provider": app.Config.Knowledge.EmbeddingProvider, "embedding_model": app.Config.Knowledge.EmbeddingModel}, "repositories": repositories})
	}})
	cmd.AddCommand(&cobra.Command{Use: "forget", Short: "Forget the local attachment without deleting Railway resources", RunE: func(c *cobra.Command, args []string) error {
		if err := app.requireYes("forget this repository's hosted Vessica attachment"); err != nil {
			return err
		}
		// Forget is a local recovery operation. In particular, it must continue
		// to work when hosted onboarding stopped after writing state.backend=hosted
		// but before the control-plane attachment became complete. Opening product
		// state here would make a dead or partial hosted attachment impossible to
		// remove.
		if err := app.loadRepositoryConfig(); err != nil {
			return err
		}
		projectID := app.Config.Hosted.ProjectID
		workspaceID := app.Config.Hosted.WorkspaceID
		if err := onboarding.RemoveInstallation(projectID); err != nil {
			return err
		}
		defaults := config.Defaults()
		app.Config.Hosted = config.HostedConfig{}
		app.Config.Attachment = config.AttachmentConfig{}
		app.Config.State = defaults.State
		app.Config.Knowledge = defaults.Knowledge
		app.Config.Sandbox.Backend = "railway"
		if err := config.Save(app.Root, app.Config); err != nil {
			return err
		}
		if err := auth.DeleteSecret(railwaySecretsReference(app.Root)); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"forgotten": projectID != "" || workspaceID != "", "railway_project_deleted": false, "project_id": projectID, "workspace_id": workspaceID})
	}})
	return cmd
}

func newIntegrationCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "integration", Short: "Connect optional hosted integrations"}
	var opts linearIntegrationOptions
	connect := &cobra.Command{Use: "connect <provider>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if args[0] != "linear" {
			return app.Printer.Fail("unsupported_integration", "only Linear is currently supported", "")
		}
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return app.Printer.Fail("repository_not_attached", err.Error(), "run ves up first")
		}
		defer app.closeDB()
		if app.Config.Hosted.ControlPlaneURL == "" || app.Config.Hosted.ProjectID == "" {
			return app.Printer.Fail("hosted_workspace_required", "Linear connects to a hosted Vessica workspace", "run ves up first")
		}
		if app.Flags.DryRun {
			return app.dryRun("integration.connect.linear", map[string]any{"workspace_id": app.Config.Hosted.WorkspaceID, "railway_project_id": app.Config.Hosted.ProjectID, "linear_team": opts.Team, "linear_project": opts.Project, "actions": []string{"authenticate Linear", "discover team, project, and workflow states", "create the Vessica trigger label and signed webhook when absent", "update Linear control-plane variables", "redeploy only the control plane and verify readiness"}})
		}
		if err := app.requireYes("connect Linear and redeploy the hosted control plane"); err != nil {
			return err
		}
		if _, err := auth.Token("linear"); err != nil {
			if _, err = loginProvider(c.Context(), c, app, "linear", "", "", ""); err != nil {
				return app.Printer.Fail("linear_authentication_failed", err.Error(), "authenticate with ves auth login linear, then retry")
			}
		}
		result, err := connectLinearIntegration(c.Context(), app, opts)
		if err != nil {
			return app.Printer.Fail("linear_connection_failed", err.Error(), "fix the reported Linear or Railway prerequisite, then rerun this command")
		}
		return app.Printer.Success(result)
	}}
	connect.Flags().StringVar(&opts.Team, "team", "", "Linear team id, key, or name")
	connect.Flags().StringVar(&opts.Project, "project", "", "default Linear project id, slug, or name")
	connect.Flags().StringVar(&opts.TodoState, "todo-state", "", "Linear Todo state id or name")
	connect.Flags().StringVar(&opts.WIPState, "wip-state", "", "Linear WIP state id or name")
	connect.Flags().StringVar(&opts.DoneState, "done-state", "", "Linear Done state id or name")
	connect.Flags().StringVar(&opts.BlockedState, "blocked-state", "", "optional Linear blocked state id or name")
	connect.Flags().StringVar(&opts.TriggerLabel, "trigger-label", "", "only process issues with this label")
	var switchProject string
	switchCmd := &cobra.Command{Use: "switch-project <provider>", Args: cobra.ExactArgs(1), Short: "Change the default Linear project", RunE: func(c *cobra.Command, args []string) error {
		if args[0] != "linear" {
			return app.Printer.Fail("unsupported_integration", "only Linear is currently supported", "")
		}
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return app.Printer.Fail("repository_not_attached", err.Error(), "run ves up first")
		}
		defer app.closeDB()
		if app.Config.Hosted.ControlPlaneURL == "" || app.Config.Hosted.ProjectID == "" {
			return app.Printer.Fail("hosted_workspace_required", "Linear connects to a hosted Vessica workspace", "run ves up first")
		}
		if app.Flags.DryRun {
			return app.dryRun("integration.switch-project.linear", map[string]any{"linear_project": switchProject, "actions": []string{"resolve the project in the connected Linear team", "update the default project", "redeploy only the control plane and verify readiness"}})
		}
		if err := app.requireYes("switch the default Linear project and redeploy the hosted control plane"); err != nil {
			return err
		}
		if _, err := auth.Token("linear"); err != nil {
			if _, err = loginProvider(c.Context(), c, app, "linear", "", "", ""); err != nil {
				return app.Printer.Fail("linear_authentication_failed", err.Error(), "authenticate with ves auth login linear, then retry")
			}
		}
		result, err := connectLinearIntegration(c.Context(), app, linearIntegrationOptions{Project: switchProject})
		if err != nil {
			return app.Printer.Fail("linear_project_switch_failed", err.Error(), "verify the project belongs to the configured Linear team, then retry")
		}
		return app.Printer.Success(result)
	}}
	switchCmd.Flags().StringVar(&switchProject, "project", "", "Linear project id, slug, or name")
	_ = switchCmd.MarkFlagRequired("project")
	cmd.AddCommand(connect, switchCmd)
	return cmd
}

func newDevCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "dev", Short: "Local-only Vessica development and test utilities"}
	up := newInitCmd(app)
	up.Use = "up"
	up.Short = "Create a local developer workspace"
	cmd.AddCommand(up)
	cmd.AddCommand(&cobra.Command{Use: "reset", Short: "Reset local developer state", RunE: func(c *cobra.Command, args []string) error {
		if err := app.requireYes("delete local Vessica developer state"); err != nil {
			return err
		}
		root, err := config.FindRoot(app.Root)
		if err != nil {
			return err
		}
		stateDir := filepath.Join(root, config.DirName, "state")
		if err := os.RemoveAll(stateDir); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"reset": true, "path": stateDir})
	}})
	return cmd
}
