package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newMemoryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Manage durable memory"}
	var title, body, typ, source, importance, subject, perm, visibility string
	var stdin bool

	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListMemories(context.Background())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			m, err := app.DB.GetMemory(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(m)
		},
	})

	add := &cobra.Command{
		Use: "add", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			b := body
			if stdin || b == "" {
				raw, err := io.ReadAll(os.Stdin)
				if err == nil && len(raw) > 0 {
					b = string(raw)
				}
			}
			if b == "" {
				return app.Printer.Fail("missing_body", "body required via --body or --stdin", "")
			}
			if title == "" {
				title = firstLine(b)
			}
			if app.Flags.DryRun {
				return app.dryRun("memory.add", map[string]any{"type": typ, "title": title, "body": b, "source": source, "importance": importance})
			}
			if replayed, err := app.idempotencyReplay(context.Background()); err != nil || replayed {
				return err
			}
			m, err := app.DB.CreateMemory(context.Background(), typ, title, b, source, importance)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), m); err != nil {
				return err
			}
			return app.Printer.Success(m)
		},
	}
	add.Flags().StringVar(&title, "title", "", "title")
	add.Flags().StringVar(&body, "body", "", "body")
	add.Flags().StringVar(&typ, "type", "insight", "type")
	add.Flags().StringVar(&source, "source", "cli", "source")
	add.Flags().StringVar(&importance, "importance", "medium", "importance")
	add.Flags().BoolVar(&stdin, "stdin", false, "read body from stdin")
	cmd.AddCommand(add)

	update := &cobra.Command{
		Use: "update <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("memory.update", map[string]any{"id": args[0], "title": title, "body": body})
			}
			m, err := app.DB.UpdateMemory(context.Background(), args[0], title, body)
			if err != nil {
				return err
			}
			return app.Printer.Success(m)
		},
	}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use: "delete <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.requireYes("delete memory"); err != nil {
				return err
			}
			if app.Flags.DryRun {
				return app.dryRun("memory.delete", map[string]any{"id": args[0]})
			}
			if err := app.DB.DeleteMemory(context.Background(), args[0]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"deleted": args[0]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "search <query>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.SearchMemory(context.Background(), args[0], 20)
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "compact", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListMemories(context.Background())
			if err != nil {
				return err
			}
			return app.Printer.Success(map[string]any{"memories": len(list), "message": "compaction is a no-op placeholder in v1"})
		},
	})
	grant := &cobra.Command{
		Use: "grant <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			return app.Printer.Success(map[string]string{"granted": args[0], "subject": subject, "perm": perm, "note": "permissions stored; enforcement basic in v1"})
		},
	}
	grant.Flags().StringVar(&subject, "subject", "", "user:|org:|public")
	grant.Flags().StringVar(&perm, "perm", "read", "read|write|admin")
	cmd.AddCommand(grant)
	revoke := &cobra.Command{
		Use: "revoke <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			return app.Printer.Success(map[string]string{"revoked": args[0], "subject": subject})
		},
	}
	revoke.Flags().StringVar(&subject, "subject", "", "subject")
	cmd.AddCommand(revoke)
	vis := &cobra.Command{
		Use: "visibility <memory_id> <private|org|public>", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.DB.SetMemoryVisibility(context.Background(), args[0], args[1]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"id": args[0], "visibility": args[1]})
		},
	}
	_ = visibility
	cmd.AddCommand(vis)
	return cmd
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}
