package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRailwayPreviewSessionCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "preview-session",
		Short: "Authorize and test the hosted Railway preview-forwarding session",
	}
	for _, action := range []string{"authorize", "status", "repair-key", "smoke"} {
		action := action
		root.AddCommand(&cobra.Command{
			Use:   action,
			Short: railwayPreviewSessionShort(action),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := app.loadWorkspaceWithoutGC(cmd.Context()); err != nil {
					return err
				}
				defer app.closeDB()
				cfg := app.Config
				if cfg.Hosted.ProjectID == "" || cfg.Hosted.EnvironmentID == "" || cfg.Hosted.ServiceID == "" || cfg.Hosted.WorkspaceID == "" {
					return fmt.Errorf("hosted Railway workspace, project, environment, and service are required")
				}
				remote := []string{
					"ssh", "--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID, "--service", cfg.Hosted.ServiceID,
					"--", "ves", "control-plane", "railway-session", action, "--workspace-id", cfg.Hosted.WorkspaceID,
				}
				return runRailwaySessionStreaming(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), remote...)
			},
		})
	}
	return root
}

func railwayPreviewSessionShort(action string) string {
	switch action {
	case "authorize":
		return "Open a device-code login inside the deployed control plane"
	case "status":
		return "Validate the durable forwarding session"
	case "repair-key":
		return "Rotate and register the forwarding key for the authorized session"
	default:
		return "Run the deployed control-plane to Railway relay to sandbox smoke test"
	}
}
