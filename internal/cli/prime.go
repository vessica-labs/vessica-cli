package cli

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/prime"
)

func newPrimeCmd(app *App) *cobra.Command {
	var forRunner, epicID, ticketID string
	var minimal bool
	cmd := &cobra.Command{
		Use:   "prime",
		Short: "Prime agent/human context",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			resp, err := prime.Build(context.Background(), app.DB, app.Root, prime.Request{
				For:      forRunner,
				EpicID:   epicID,
				TicketID: ticketID,
				Minimal:  minimal,
			})
			if err != nil {
				return err
			}
			if app.Flags.JSON {
				return app.Printer.Success(resp)
			}
			return app.Printer.Success(prime.FormatHuman(resp))
		},
	}
	cmd.Flags().StringVar(&forRunner, "for", "", "claude|codex|cursor|pi")
	cmd.Flags().StringVar(&epicID, "epic", "", "epic id")
	cmd.Flags().StringVar(&ticketID, "ticket", "", "ticket id")
	cmd.Flags().BoolVar(&minimal, "minimal", false, "minimal context")
	return cmd
}
