package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

// GlobalFlags are shared across commands.
type GlobalFlags struct {
	JSON           bool
	NoColor        bool
	Quiet          bool
	Verbose        bool
	Debug          bool
	CWD            string
	ConfigPath     string
	Yes            bool
	DryRun         bool
	IdempotencyKey string
}

func (a *App) openKnowledge(ctx context.Context) (*knowledgegateway.Gateway, error) {
	workspaceID := a.Config.Knowledge.WorkspaceID
	if workspaceID == "" {
		workspaceID = a.Config.Attachment.WorkspaceID
	}
	if workspaceID == "" && a.DB != nil {
		ws, err := a.DB.GetWorkspace(ctx)
		if err != nil {
			return nil, err
		}
		workspaceID = ws.ID
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("knowledge workspace identity is not configured")
	}
	return knowledgegateway.Open(a.Root, a.Config, workspaceID)
}

// App is the CLI application context.
type App struct {
	Flags   GlobalFlags
	Printer *output.Printer
	Root    string
	Config  config.Config
	DB      *state.DB
}

func NewRoot() *cobra.Command {
	app := &App{}
	root := &cobra.Command{
		Use:           "ves",
		Short:         "Vessica — hosted engineering control plane for coding agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			app.Printer = output.New(app.Flags.JSON, app.Flags.Quiet, app.Flags.NoColor)
			cwd := app.Flags.CWD
			if cwd == "" {
				var err error
				cwd, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			abs, err := filepath.Abs(cwd)
			if err != nil {
				return err
			}
			app.Root = abs
			return nil
		},
	}

	f := root.PersistentFlags()
	f.BoolVar(&app.Flags.JSON, "json", false, "machine-safe JSON output")
	f.BoolVar(&app.Flags.NoColor, "no-color", false, "disable color")
	f.BoolVar(&app.Flags.Quiet, "quiet", false, "minimal output")
	f.BoolVar(&app.Flags.Verbose, "verbose", false, "verbose output")
	f.BoolVar(&app.Flags.Debug, "debug", false, "debug output")
	f.StringVar(&app.Flags.CWD, "cwd", "", "working directory")
	f.StringVar(&app.Flags.ConfigPath, "config", "", "config file path")
	f.BoolVar(&app.Flags.Yes, "yes", false, "skip confirmation prompts")
	f.BoolVar(&app.Flags.DryRun, "dry-run", false, "show actions without mutating")
	f.StringVar(&app.Flags.IdempotencyKey, "idempotency-key", "", "idempotency key for mutating commands")

	root.AddCommand(newUpCmd(app))
	root.AddCommand(newDevCmd(app))
	root.AddCommand(newWorkspaceCmd(app))
	root.AddCommand(newIntegrationCmd(app))
	root.AddCommand(newStatusCmd(app))
	root.AddCommand(newDoctorCmd(app))
	root.AddCommand(newToolchainCmd(app))
	root.AddCommand(newCapabilitiesCmd(app))
	root.AddCommand(newConfigCmd(app))
	root.AddCommand(newAuthCmd(app))
	root.AddCommand(newSetupCmd(app))
	root.AddCommand(newPackCmd(app))
	root.AddCommand(newHarnessCmd(app))
	root.AddCommand(newEpicCmd(app))
	root.AddCommand(newArtifactCmd(app))
	root.AddCommand(newTicketCmd(app))
	root.AddCommand(newWaveCmd(app))
	root.AddCommand(newRunCmd(app))
	root.AddCommand(newSandboxCmd(app))
	root.AddCommand(newRepoCmd(app))
	root.AddCommand(newTrackerCmd(app))
	root.AddCommand(newMemoryCmd(app))
	root.AddCommand(newKnowledgeCmd(app))
	root.AddCommand(newEntityCmd(app))
	root.AddCommand(newPrimeCmd(app))
	root.AddCommand(newReceiptCmd(app))
	root.AddCommand(newTraceCmd(app))
	root.AddCommand(newControlPlaneCmd(app))
	root.AddCommand(newRailwayCmd(app))
	root.AddCommand(newDashboardCmd(app))
	root.AddCommand(newVersionCmd(app))

	return root
}

func (a *App) loadWorkspace(ctx context.Context) error {
	return a.loadWorkspaceWithGC(ctx, true)
}

func (a *App) loadWorkspaceWithoutGC(ctx context.Context) error {
	return a.loadWorkspaceWithGC(ctx, false)
}

func (a *App) loadWorkspaceWithGC(ctx context.Context, autoGC bool) error {
	if err := a.loadRepositoryConfig(); err != nil {
		return err
	}
	if config.IsHostedAttachment(a.Config) {
		// Hosted control-plane state is the sole product authority. Individual
		// commands use the authenticated gateway and must never open a local
		// fallback database merely because an attachment exists.
		return nil
	}
	db, err := state.Open(a.Config.State.Backend, a.Config.State.DBURL, a.Root)
	if err != nil {
		return err
	}
	a.DB = db
	if _, err = db.GetWorkspace(ctx); err != nil {
		return err
	}
	if autoGC && !a.Flags.DryRun {
		gcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, _ = retention.GC(gcCtx, db, a.Root, retention.GCOptions{})
		cancel()
	}
	return nil
}

func (a *App) loadRepositoryConfig() error {
	root, err := config.FindRoot(a.Root)
	if err != nil {
		return err
	}
	a.Root = root
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	config.ApplyEnv(&cfg)
	config.EnforceHostedAuthority(&cfg)
	a.Config = cfg
	return nil
}

func (a *App) closeDB() {
	if a.DB != nil {
		_ = a.DB.Close()
	}
}

func (a *App) requireYes(action string) error {
	if a.Flags.Yes {
		return nil
	}
	if a.Flags.JSON {
		return a.Printer.Fail("confirmation_required", "confirmation required to "+action, "review the action, then repeat with --yes")
	}
	return fmt.Errorf("refusing to %s without --yes", action)
}

func (a *App) dryRun(action string, data any) error {
	return a.Printer.Success(map[string]any{
		"dry_run": true,
		"action":  action,
		"would":   data,
	})
}

func (a *App) idempotencyReplay(ctx context.Context) (bool, error) {
	data, ok, err := a.idempotencyLookup(ctx)
	if err != nil || !ok {
		return ok, err
	}
	return true, a.Printer.Success(data)
}

func (a *App) idempotencyLookup(ctx context.Context) (any, bool, error) {
	if a.Flags.IdempotencyKey == "" || a.DB == nil {
		return nil, false, nil
	}
	raw, ok, err := a.DB.GetIdempotency(ctx, a.Flags.IdempotencyKey)
	if err != nil || !ok {
		return nil, ok, err
	}
	var data any = raw
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		data = decoded
	}
	return data, true, nil
}

func (a *App) idempotencyStore(ctx context.Context, data any) error {
	if a.Flags.IdempotencyKey == "" || a.DB == nil {
		return nil
	}
	return a.DB.PutIdempotency(ctx, a.Flags.IdempotencyKey, data)
}

func newVersionCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.Printer.Success(map[string]string{"version": version.Version, "product": "vessica-cli"})
		},
	}
}
