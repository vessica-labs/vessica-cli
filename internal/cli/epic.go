package cli

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/run"
)

func newEpicCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "epic", Short: "Manage epics"}
	var title, body, bodyFile, status string

	add := &cobra.Command{
		Use: "add", Short: "Create an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			b := body
			if bodyFile != "" {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				b = string(raw)
			}
			if b == "" && !isTTY() {
				raw, _ := os.ReadFile("/dev/stdin")
				b = string(raw)
			}
			if title == "" {
				return app.Printer.Fail("missing_title", "--title required", "")
			}
			if app.Flags.DryRun {
				return app.dryRun("epic.add", map[string]any{"title": title, "body": b})
			}
			if replayed, err := app.idempotencyReplay(context.Background()); err != nil || replayed {
				return err
			}
			e, err := app.DB.CreateEpic(context.Background(), title, b)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), e); err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	}
	add.Flags().StringVar(&title, "title", "", "epic title")
	add.Flags().StringVar(&body, "body", "", "epic body")
	add.Flags().StringVar(&bodyFile, "body-file", "", "read body from file")
	cmd.AddCommand(add)

	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListEpics(context.Background())
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatEpics(list))
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			e, err := app.DB.GetEpic(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	})
	update := &cobra.Command{
		Use: "update <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			e, err := app.DB.UpdateEpic(context.Background(), args[0], title, body, status)
			if err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	update.Flags().StringVar(&status, "status", "", "status")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use: "delete <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.requireYes("delete epic"); err != nil {
				return err
			}
			if app.Flags.DryRun {
				return app.dryRun("epic.delete", map[string]any{"id": args[0]})
			}
			if err := app.DB.DeleteEpic(context.Background(), args[0]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"deleted": args[0]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "plan <epic_id>", Args: cobra.ExactArgs(1), Short: "Plan epic through ticketize",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: !app.Flags.JSON}
			r, err := eng.RunEpic(context.Background(), run.Options{
				EpicID:    args[0],
				StopAfter: "ticketize",
				Stream:    !app.Flags.JSON,
			})
			if err != nil {
				return err
			}
			return app.Printer.Success(r)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			e, err := app.DB.GetEpic(context.Background(), args[0])
			if err != nil {
				return err
			}
			tickets, _ := app.DB.ListTickets(context.Background(), e.ID)
			ready, _ := app.DB.ReadyTickets(context.Background(), e.ID)
			return app.Printer.Success(map[string]any{"epic": e, "tickets": len(tickets), "ready": len(ready)})
		},
	})
	_ = strings.TrimSpace
	return cmd
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
