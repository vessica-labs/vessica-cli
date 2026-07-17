package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

func newRunArtifactsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use: "artifacts <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				arts, err := app.getHostedRunArtifacts(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if !app.Flags.JSON {
					return app.Printer.Success(formatArtifacts(arts))
				}
				return app.Printer.Success(arts)
			}
			runRecord, err := app.DB.GetRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			arts, err := app.DB.ListArtifactsForRun(cmd.Context(), runRecord.ID)
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatArtifacts(arts))
			}
			return app.Printer.Success(arts)
		},
	}
}

func newRunReceiptCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use: "receipt <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				runRecord, err := app.getHostedRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if runRecord.ReceiptID == "" {
					return app.Printer.Fail("no_receipt", "run has no receipt yet", "")
				}
				view, err := app.getHostedReceipt(cmd.Context(), runRecord.ReceiptID)
				if err != nil {
					return err
				}
				return app.Printer.Success(view)
			}
			runRecord, err := app.DB.GetRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if runRecord.ReceiptID == "" {
				return app.Printer.Fail("no_receipt", "run has no receipt yet", "")
			}
			record, err := app.DB.GetReceipt(cmd.Context(), runRecord.ReceiptID)
			if err != nil {
				return err
			}
			var body any
			_ = json.Unmarshal([]byte(record.BodyJSON), &body)
			return app.Printer.Success(map[string]any{"receipt": record, "body": body})
		},
	}
}
