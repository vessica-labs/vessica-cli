package cli

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
)

func newReceiptCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "receipt", Short: "View receipts"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListReceipts(context.Background())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <receipt_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			rcpt, err := app.DB.GetReceipt(context.Background(), args[0])
			if err != nil {
				return err
			}
			view, err := receipt.ViewJSON(rcpt)
			if err != nil {
				return err
			}
			return app.Printer.Success(view)
		},
	})
	return cmd
}

func newTraceCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "trace", Short: "View traces"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListTraces(context.Background())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <trace_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.GetTrace(context.Background(), args[0])
			if err != nil {
				return err
			}
			var body any
			_ = json.Unmarshal([]byte(t.BodyJSON), &body)
			return app.Printer.Success(map[string]any{"trace": t, "body": body})
		},
	})
	return cmd
}

func saveWorkspaceConfig(app *App) error {
	return config.Save(app.Root, app.Config)
}
