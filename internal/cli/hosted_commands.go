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
	"github.com/vessica-labs/vessica-cli/internal/tracker"
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
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		projectID := app.Config.Hosted.ProjectID
		workspaceID := app.Config.Hosted.WorkspaceID
		if err := onboarding.RemoveInstallation(projectID); err != nil {
			return err
		}
		defaults := config.Defaults()
		app.Config.Hosted = config.HostedConfig{}
		app.Config.Attachment = config.AttachmentConfig{}
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
	cmd.AddCommand(&cobra.Command{Use: "connect <provider>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
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
			return app.dryRun("integration.connect.linear", map[string]any{"workspace_id": app.Config.Hosted.WorkspaceID, "project_id": app.Config.Hosted.ProjectID, "actions": []string{"authenticate Linear", "discover team and workflow states", "create the Vessica trigger label", "configure the hosted control plane", "create a signed webhook", "redeploy and verify readiness"}})
		}
		if _, err := auth.Token("linear"); err != nil {
			if _, err = loginProvider(c.Context(), c, app, "linear", "", "", ""); err != nil {
				return app.Printer.Fail("linear_authentication_failed", err.Error(), "authenticate with ves auth login linear, then retry")
			}
		}
		result, err := railwayUp(c.Context(), app, railwayUpOptions{Name: "vessica", Workspace: app.Config.Hosted.WorkspaceID, Image: firstNonEmpty(app.Config.Hosted.ControlPlaneImage, defaultControlPlaneImage()), EnableLinear: true})
		if err != nil {
			return app.Printer.Fail("linear_connection_failed", err.Error(), "fix the reported Linear or Railway prerequisite, then rerun this command")
		}
		if result["status"] != "running" {
			return app.Printer.Fail("linear_connection_incomplete", "hosted prerequisites are incomplete", "authenticate the listed providers, then retry")
		}
		status, err := tracker.Connect("linear")
		if err != nil {
			return err
		}
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return err
		}
		if err := saveHostedClientConfig(app.Config, secrets); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"integration": status, "deployment": result})
	}})
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
