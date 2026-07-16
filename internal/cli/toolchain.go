package cli

import (
	"github.com/spf13/cobra"
	agenttoolchain "github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func newToolchainCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "toolchain", Short: "Inspect the coding-agent toolchain"}
	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Verify every required coding-agent tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := agenttoolchain.Verify(cmd.Context(), app.Root)
			return app.Printer.Success(map[string]any{
				"ok":          agenttoolchain.AllOK(checks),
				"profile":     "sandbox-base-v1",
				"fingerprint": agenttoolchain.Fingerprint(),
				"tools":       checks,
			})
		},
	})
	return cmd
}
