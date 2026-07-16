package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	agenttoolchain "github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func newToolchainCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "toolchain", Short: "Inspect the coding-agent toolchain"}
	var profile string
	verify := &cobra.Command{
		Use:   "verify",
		Short: "Verify every required coding-agent tool",
		RunE: func(cmd *cobra.Command, args []string) error {
			var checks []agenttoolchain.Check
			reportedProfile := profile
			switch profile {
			case "worker":
				checks = agenttoolchain.Verify(cmd.Context(), app.Root)
				reportedProfile = "sandbox-base-v1"
			case "workstation":
				checks = agenttoolchain.VerifyWorkstation(cmd.Context(), app.Root)
			default:
				return fmt.Errorf("unsupported toolchain profile %q; use worker or workstation", profile)
			}
			return app.Printer.Success(map[string]any{
				"ok":          agenttoolchain.AllOK(checks),
				"profile":     reportedProfile,
				"fingerprint": agenttoolchain.Fingerprint(),
				"tools":       checks,
			})
		},
	}
	verify.Flags().StringVar(&profile, "profile", "worker", "toolchain profile: worker or workstation")
	cmd.AddCommand(verify)
	return cmd
}
