package cli

import (
	"context"
	"os"

	"github.com/spf13/cobra"
)

func newArtifactCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "artifact", Short: "Manage artifacts"}
	var epicID, typ, title, body, bodyFile string

	list := &cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListArtifacts(context.Background(), epicID, typ)
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatArtifacts(list))
			}
			return app.Printer.Success(list)
		},
	}
	list.Flags().StringVar(&epicID, "epic", "", "filter by epic")
	list.Flags().StringVar(&typ, "type", "", "prd|adr|design-spec|test-scenarios")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use: "view <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			a, err := app.DB.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(a)
		},
	})

	add := &cobra.Command{
		Use: "add", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if typ == "" || title == "" {
				return app.Printer.Fail("missing_fields", "--type and --title required", "")
			}
			b := body
			if bodyFile != "" {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				b = string(raw)
			}
			if app.Flags.DryRun {
				return app.dryRun("artifact.add", map[string]any{"epic": epicID, "type": typ, "title": title, "body": b})
			}
			if replayed, err := app.idempotencyReplay(context.Background()); err != nil || replayed {
				return err
			}
			a, err := app.DB.CreateArtifact(context.Background(), typ, title, b, epicID, "")
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), a); err != nil {
				return err
			}
			return app.Printer.Success(a)
		},
	}
	add.Flags().StringVar(&typ, "type", "", "artifact type")
	add.Flags().StringVar(&title, "title", "", "title")
	add.Flags().StringVar(&body, "body", "", "body")
	add.Flags().StringVar(&bodyFile, "body-file", "", "body file")
	add.Flags().StringVar(&epicID, "epic", "", "epic id")
	cmd.AddCommand(add)

	update := &cobra.Command{
		Use: "update <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("artifact.update", map[string]any{"id": args[0], "title": title, "body": body})
			}
			a, err := app.DB.UpdateArtifact(context.Background(), args[0], title, body, "")
			if err != nil {
				return err
			}
			return app.Printer.Success(a)
		},
	}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use: "approve <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("artifact.approve", map[string]any{"id": args[0]})
			}
			a, err := app.DB.ApproveArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(a)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "diff <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			a, err := app.DB.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(map[string]any{"id": a.ID, "version": a.Version, "status": a.Status, "note": "version history available in artifact_versions"})
		},
	})
	return cmd
}
