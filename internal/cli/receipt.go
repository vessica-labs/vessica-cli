package cli

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
)

func newReceiptCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "receipt", Short: "View receipts"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListReceipts(cmd.Context())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <receipt_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			rcpt, err := app.DB.GetReceipt(cmd.Context(), args[0])
			if err != nil {
				if app.Config.Hosted.ControlPlaneURL == "" {
					return err
				}
				secrets, secretErr := loadRailwaySecrets(app.Root)
				if secretErr != nil {
					return secretErr
				}
				var view any
				endpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/receipts/" + args[0]
				if requestErr := hostedRequest(cmd.Context(), http.MethodGet, endpoint, secrets.APIToken, nil, &view); requestErr != nil {
					return requestErr
				}
				return app.Printer.Success(view)
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
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListTraces(cmd.Context())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <trace_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.GetTrace(cmd.Context(), args[0])
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
	if app.Config.Attachment.RepositoryID != "" && app.Config.Hosted.ProjectID != "" {
		if secrets, err := loadRailwaySecrets(app.Root); err == nil {
			if err := saveHostedClientConfig(app.Config, secrets); err != nil {
				return err
			}
		}
	}
	return config.Save(app.Root, app.Config)
}
