package cli

import (
	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/harness"
)

func newHarnessCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "harness", Short: "Manage repo harness"}
	cmd.AddCommand(&cobra.Command{
		Use: "create", Short: "Create initial harness",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			st, err := harness.Create(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(st)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "audit", Short: "Audit harness drift",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			st, err := harness.Audit(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(st)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "sync", Short: "Sync harness to repo reality",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			st, err := harness.Sync(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(st)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "lint", Short: "Run deterministic harness lint",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			st, err := harness.Lint(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(st)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show harness status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			st, err := harness.StatusOf(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(st)
		},
	})
	return cmd
}
