package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func newRepoCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "Repository integrations"}
	var runID string

	cmd.AddCommand(&cobra.Command{
		Use: "connect <provider>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			app.Config.Repo.Provider = args[0]
			if err := saveConfig(app); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"provider": args[0]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			return app.Printer.Success(repo.Status(context.Background(), app.Root, app.Config.Repo.Remote))
		},
	})
	prCreate := &cobra.Command{
		Use: "pr", Short: "PR operations",
	}
	create := &cobra.Command{
		Use: "create", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			r, err := app.DB.GetRun(context.Background(), runID)
			if err != nil {
				return err
			}
			if r.PRURL != "" {
				return app.Printer.Success(map[string]string{"pr_url": r.PRURL})
			}
			return app.Printer.Fail("no_pr", "run has no PR; re-run with --pr draft", "")
		},
	}
	create.Flags().StringVar(&runID, "run", "", "run id")
	prCreate.AddCommand(create)
	view := &cobra.Command{
		Use: "view", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			r, err := app.DB.GetRun(context.Background(), runID)
			if err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"pr_url": r.PRURL})
		},
	}
	view.Flags().StringVar(&runID, "run", "", "run id")
	prCreate.AddCommand(view)
	cmd.AddCommand(prCreate)
	return cmd
}

func newTrackerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "tracker", Short: "Best-efforts tracker sync"}
	cmd.AddCommand(&cobra.Command{
		Use: "connect <provider>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			st, err := tracker.Connect(args[0])
			if err != nil {
				return err
			}
			if err := app.loadWorkspace(); err == nil {
				app.Config.Tracker.Provider = args[0]
				_ = saveConfig(app)
				app.closeDB()
			}
			return app.Printer.Success(st)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "sync", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			res, err := tracker.Sync(context.Background(), app.DB, app.Config.Tracker.Provider)
			if err != nil {
				return err
			}
			return app.Printer.Success(res)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
			provider := "linear"
			var dbLoaded bool
			if err := app.loadWorkspace(); err == nil {
				provider = app.Config.Tracker.Provider
				dbLoaded = true
			}
			if dbLoaded {
				defer app.closeDB()
				return app.Printer.Success(tracker.GetStatus(context.Background(), app.DB, provider))
			}
			return app.Printer.Success(tracker.GetStatus(context.Background(), nil, provider))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "push", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			epics, _ := app.DB.ListEpics(context.Background())
			var pushed int
			for _, e := range epics {
				if _, err := tracker.Push(context.Background(), app.DB, app.Config.Tracker.Provider, "epic", e.ID); err == nil {
					pushed++
				}
			}
			return app.Printer.Success(map[string]any{"pushed": pushed})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "pull", RunE: func(cmd *cobra.Command, args []string) error {
			return app.Printer.Fail("unsupported", "tracker pull is not supported in v1 launch", "Vessica is source of truth; use tracker push/sync")
		},
	})
	return cmd
}

func saveConfig(app *App) error {
	return saveWorkspaceConfig(app)
}
