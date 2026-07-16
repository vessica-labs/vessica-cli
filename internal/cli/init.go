package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func newInitCmd(app *App) *cobra.Command {
	var (
		profile string
		backend string
		dbURL   string
		sandbox string
		tracker string
		repo    string
		runner  string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a Vessica workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			root := app.Root
			vesDir := config.Dir(root)
			var cfg config.Config
			configPath := filepath.Join(vesDir, config.ConfigFile)
			existingConfig := false
			if st, err := os.Stat(configPath); err == nil && !st.IsDir() {
				cfg, err = config.Load(root)
				if err != nil {
					return err
				}
				existingConfig = true
			} else if profile == "team" {
				cfg = config.TeamDefaults()
			} else {
				cfg = config.Defaults()
			}
			if backend != "" {
				cfg.State.Backend = backend
			}
			if dbURL != "" {
				cfg.State.DBURL = dbURL
			}
			if sandbox != "" {
				cfg.Sandbox.Backend = sandbox
			}
			if tracker != "" {
				cfg.Tracker.Provider = tracker
			}
			if repo != "" {
				cfg.Repo.Provider = repo
			}
			if runner != "" {
				cfg.Runner.Default = runner
			}

			if cfg.State.Backend == "sqlite" && profile == "team" {
				app.Printer.Info("warning: SQLite is not suitable for shared team coordination; prefer --state postgres-url")
			}
			if (cfg.State.Backend == "postgres-url" || cfg.State.Backend == "postgres-docker") && cfg.State.DBURL == "" {
				return app.Printer.Fail("missing_db_url", "postgres backend requires --db-url", "pass --db-url $DATABASE_URL")
			}

			if remote := detectGitRemote(root); remote != "" && cfg.Repo.Remote == "" {
				cfg.Repo.Remote = remote
			}

			for _, d := range []string{"state", "cache", "runs", "sandboxes", "secrets", "agents", "templates", "workflows"} {
				if err := os.MkdirAll(filepath.Join(vesDir, d), 0o755); err != nil {
					return err
				}
			}
			configChanged := !existingConfig || backend != "" || dbURL != "" || sandbox != "" || tracker != "" || repo != "" || runner != ""
			if configChanged {
				if err := config.Save(root, cfg); err != nil {
					return err
				}
			}
			if err := config.EnsureGitignore(root); err != nil {
				return err
			}

			db, err := state.Open(cfg.State.Backend, cfg.State.DBURL, root)
			if err != nil {
				return err
			}
			defer db.Close()
			ws, err := db.EnsureWorkspace(cmd.Context(), root, profile)
			if err != nil {
				return err
			}

			return app.Printer.Success(map[string]any{
				"workspace_id":  ws.ID,
				"root":          root,
				"profile":       profile,
				"state":         cfg.State.Backend,
				"sandbox":       cfg.Sandbox.Backend,
				"runner":        cfg.Runner.Default,
				"repo":          cfg.Repo.Provider,
				"tracker":       cfg.Tracker.Provider,
				"config_reused": existingConfig,
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "solo", "solo|team")
	cmd.Flags().StringVar(&backend, "state", "", "sqlite|postgres-url|postgres-docker")
	cmd.Flags().StringVar(&dbURL, "db-url", "", "database URL for postgres")
	cmd.Flags().StringVar(&sandbox, "sandbox", "", "sandbox backend (docker)")
	cmd.Flags().StringVar(&tracker, "tracker", "", "linear|jira|none")
	cmd.Flags().StringVar(&repo, "repo", "", "github|gitlab")
	cmd.Flags().StringVar(&runner, "runner", "", "codex|claude|cursor|pi")
	return cmd
}

func openState(root string, cfg config.Config) (*state.DB, error) {
	return state.Open(cfg.State.Backend, cfg.State.DBURL, root)
}

func detectGitRemote(root string) string {
	out, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func newStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workspace status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return app.Printer.Fail("not_initialized", err.Error(), "run ves init")
			}
			defer app.closeDB()
			ws, _ := app.DB.GetWorkspace(cmd.Context())
			packLock := fileExists(filepath.Join(app.Root, app.Config.Pack.Lockfile))
			return app.Printer.Success(map[string]any{
				"workspace_id": ws.ID,
				"root":         app.Root,
				"profile":      ws.Profile,
				"config":       config.Flatten(app.Config),
				"pack_lock":    packLock,
				"harness":      fileExists(filepath.Join(app.Root, "AGENTS.md")),
			})
		},
	}
}

func newDoctorCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose workspace readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := []map[string]any{}
			add := func(name string, ok bool, detail string) {
				checks = append(checks, map[string]any{"name": name, "ok": ok, "detail": detail})
			}

			root, err := config.FindRoot(app.Root)
			if err != nil {
				add("workspace", false, err.Error())
				return app.Printer.Success(map[string]any{"ok": false, "checks": checks})
			}
			app.Root = root
			add("workspace", true, root)

			cfg, err := config.Load(root)
			if err != nil {
				add("config", false, err.Error())
			} else {
				app.Config = cfg
				add("config", true, cfg.State.Backend)
			}

			db, err := openState(root, app.Config)
			if err != nil {
				add("state", false, err.Error())
			} else {
				defer db.Close()
				if err := db.Ping(cmd.Context()); err != nil {
					add("state", false, err.Error())
				} else {
					add("state", true, app.Config.State.Backend)
				}
				if ws, wsErr := db.GetWorkspace(cmd.Context()); wsErr == nil {
					if gateway, knowledgeErr := knowledgegateway.Open(root, app.Config, ws.ID); knowledgeErr != nil {
						add("knowledge", false, knowledgeErr.Error())
					} else {
						defer gateway.Close()
						if _, exportErr := gateway.Export(cmd.Context()); exportErr != nil {
							add("knowledge", false, exportErr.Error())
						} else {
							add("knowledge", true, gateway.Mode())
						}
					}
				}
			}

			if _, err := exec.LookPath("docker"); err != nil {
				add("docker", false, "docker not found in PATH")
			} else {
				out, err := exec.Command("docker", "info").CombinedOutput()
				if err != nil {
					add("docker", false, strings.TrimSpace(string(out)))
				} else {
					add("docker", true, "available")
				}
			}

			for _, check := range toolchain.Verify(cmd.Context(), root) {
				detail := check.Version
				if !check.OK {
					detail = check.Error
				}
				add("toolchain."+check.Name, check.OK, detail)
			}

			add("pack_lock", fileExists(filepath.Join(root, app.Config.Pack.Lockfile)), app.Config.Pack.Lockfile)
			add("harness_agents", fileExists(filepath.Join(root, "AGENTS.md")), "AGENTS.md")
			add("harness_yaml", fileExists(filepath.Join(root, config.DirName, config.HarnessFile)), config.HarnessFile)

			authOK := false
			if ud, err := config.UserDir(); err == nil {
				authOK = fileExists(filepath.Join(ud, "secrets", "github.token"))
			}
			add("auth_github", authOK, "ves auth login github")

			if app.Config.Repo.Remote == "" {
				add("repo_remote", false, "no repo.remote configured")
			} else {
				add("repo_remote", true, app.Config.Repo.Remote)
			}

			allOK := true
			for _, c := range checks {
				if ok, _ := c["ok"].(bool); !ok {
					allOK = false
					break
				}
			}
			return app.Printer.Success(map[string]any{"ok": allOK, "checks": checks})
		},
	}
}

func newConfigCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage workspace config"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List config keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			return app.Printer.Success(config.Flatten(app.Config))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			v, err := config.Get(app.Config, args[0])
			if err != nil {
				return app.Printer.Fail("unknown_key", err.Error(), "")
			}
			return app.Printer.Success(map[string]string{"key": args[0], "value": v})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if err := config.Set(&app.Config, args[0], args[1]); err != nil {
				return app.Printer.Fail("invalid_key", err.Error(), "")
			}
			if err := config.Save(app.Root, app.Config); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"key": args[0], "value": args[1]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if err := config.Unset(&app.Config, args[0]); err != nil {
				return app.Printer.Fail("invalid_key", err.Error(), "")
			}
			if err := config.Save(app.Root, app.Config); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"unset": args[0]})
		},
	})
	return cmd
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
