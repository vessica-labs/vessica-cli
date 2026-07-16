package cli

import (
	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/pack"
)

func newPackCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "pack", Short: "Manage agent packs"}
	cmd.AddCommand(&cobra.Command{
		Use:   "install [pack-ref]",
		Short: "Install a pack (default @vessica/engineering-harness)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			ref := pack.DefaultRef
			if len(args) > 0 {
				ref = args[0]
			}
			lock, err := pack.Install(app.Root, ref)
			if err != nil {
				return err
			}
			return app.Printer.Success(lock)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pull <git-url>",
		Short: "Install a pack from a Git URL with an optional #ref",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			lock, err := pack.Install(app.Root, args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(lock)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "sync", Short: "Sync pack from origin/embedded",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			lock, err := pack.Sync(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(lock)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "update", Short: "Explicitly update pack",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			lock, err := pack.Update(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(lock)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "pin <version-or-sha>",
		Args:  cobra.ExactArgs(1),
		Short: "Pin pack version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			lock, err := pack.Pin(app.Root, args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(lock)
		},
	})
	origin := &cobra.Command{Use: "origin", Short: "Pack origin"}
	origin.AddCommand(&cobra.Command{
		Use: "get", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			o, err := pack.OriginGet(app.Root)
			if err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"origin": o})
		},
	})
	origin.AddCommand(&cobra.Command{
		Use: "set <git-url>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if err := pack.OriginSet(app.Root, args[0]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"origin": args[0]})
		},
	})
	cmd.AddCommand(origin)
	return cmd
}
