package cli

import (
	"context"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

func newCapabilitiesCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use: "capabilities", Short: "Report the machine-readable Vessica agent contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			result := map[string]any{
				"product": "vessica-cli", "version": version.Version,
				"schema": output.EnvelopeSchema, "stream_schema": "vessica.stream/v1",
				"commands": []string{
					"capabilities", "doctor", "prime", "harness install", "harness status", "harness audit", "harness sync",
					"epic draft", "epic add", "epic view", "epic list", "epic update",
					"ticket add", "ticket list", "ticket update", "ticket block", "ticket unblock",
					"run epic", "run view", "run list", "run watch", "run cancel", "run resume", "run prompt", "run approve", "run rollback",
					"receipt view",
					"knowledge status", "knowledge context", "knowledge promote", "knowledge export", "knowledge import",
					"entity create", "entity resolve", "entity search", "artifact create", "artifact get", "artifact list", "artifact activate", "artifact supersede",
					"memory add", "memory get", "memory list", "memory search", "memory supersede", "memory archive",
				},
				"contract":       map[string]any{"json": true, "jsonl": true, "dry_run": true, "idempotency_keys": true, "structured_confirmation": true},
				"tools":          map[string]bool{"ves": true, "codex": commandAvailable("codex"), "git": commandAvailable("git"), "docker": commandAvailable("docker")},
				"authentication": auth.StatusAll([]string{"github", "linear", "railway", "codex", "knowledge"}),
				"workspace":      map[string]any{"initialized": false, "root": app.Root},
			}
			if root, err := config.FindRoot(app.Root); err == nil {
				cfg, loadErr := config.Load(root)
				if loadErr == nil {
					workspace := map[string]any{
						"initialized": true, "root": root, "state": cfg.State.Backend, "sandbox": cfg.Sandbox.Backend,
						"runner": cfg.Runner.Default, "tracker": cfg.Tracker.Provider, "repo": cfg.Repo.Provider,
						"hosted": cfg.Hosted.ControlPlaneURL != "", "control_plane_url_configured": cfg.Hosted.ControlPlaneURL != "",
						"knowledge_mode": cfg.Knowledge.Mode, "knowledge_endpoint_configured": cfg.Knowledge.Endpoint != "",
						"harness_installed": fileExists(filepath.Join(root, cfg.Pack.Lockfile)),
					}
					if db, openErr := openState(root, cfg); openErr == nil {
						defer db.Close()
						if ws, wsErr := db.GetWorkspace(context.Background()); wsErr == nil {
							workspace["workspace_id"] = ws.ID
							workspace["profile"] = ws.Profile
						}
					}
					result["workspace"] = workspace
				}
			}
			return app.Printer.Success(result)
		},
	}
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
