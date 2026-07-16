package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/codexplugin"
)

const managedBegin = "<!-- VESSICA:BEGIN -->"
const managedEnd = "<!-- VESSICA:END -->"

func newSetupCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "Install managed guidance for coding agents"}
	for _, runner := range []string{"codex", "claude", "cursor", "pi", "mcp"} {
		r := runner
		var installPlugin, check bool
		setupCmd := &cobra.Command{
			Use:   r,
			Short: "Setup " + r,
			RunE: func(cmd *cobra.Command, args []string) error {
				if r == "codex" && check {
					status := codexplugin.Status()
					status["codex_available"] = commandAvailable("codex")
					status["ves_available"] = commandAvailable("ves")
					return app.Printer.Success(status)
				}
				if err := app.loadWorkspace(cmd.Context()); err != nil {
					return err
				}
				defer app.closeDB()
				path, err := setupTarget(app.Root, r)
				if err != nil {
					return err
				}
				guidance := managedGuidance(r)
				if err := upsertManagedSection(path, guidance); err != nil {
					return err
				}
				result := map[string]any{"runner": r, "file": path, "updated": true}
				if r == "codex" && installPlugin {
					installed, err := codexplugin.Install()
					if err != nil {
						return err
					}
					result["plugin"] = installed
				}
				result["next_actions"] = []string{"ves capabilities --json", "ves doctor --json"}
				return app.Printer.Success(result)
			},
		}
		if r == "codex" {
			setupCmd.Flags().BoolVar(&installPlugin, "plugin", false, "install or update the Vessica Codex plugin")
			setupCmd.Flags().BoolVar(&check, "check", false, "check Codex and plugin installation without writing")
		}
		cmd.AddCommand(setupCmd)
	}
	return cmd
}

func setupTarget(root, runner string) (string, error) {
	switch runner {
	case "codex":
		return filepath.Join(root, "AGENTS.md"), nil
	case "claude":
		return filepath.Join(root, "CLAUDE.md"), nil
	case "cursor":
		dir := filepath.Join(root, ".cursor", "rules")
		_ = os.MkdirAll(dir, 0o755)
		return filepath.Join(dir, "vessica.mdc"), nil
	case "pi":
		return filepath.Join(root, "PI.md"), nil
	case "mcp":
		return filepath.Join(root, ".vessica", "mcp.md"), nil
	default:
		return "", fmt.Errorf("unknown runner")
	}
}

func managedGuidance(runner string) string {
	return fmt.Sprintf(`# Vessica (%s)

Use the Vessica CLI for durable project state.

Engine-managed runs:
- When invoked by `+"`ves run epic`"+`, Vessica owns ticket lifecycle and run state.
- Do not run `+"`ves ticket claim`"+`, `+"`ves ticket close`"+`, `+"`ves ticket heartbeat`"+`, `+"`ves ticket release`"+`, or `+"`ves memory add`"+` from inside the coding task.
- Implement the requested change, run relevant local checks, and return a concise evidence summary.

Standalone/manual agent commands:
- ves prime --json
- ves ticket ready --json
- ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m --json
- ves memory add --stdin --json
- ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id> --json

Do not create ad hoc TODO/plan/memory files when Vessica is initialized.
`, runner)
}

func upsertManagedSection(path, body string) error {
	section := managedBegin + "\n" + body + "\n" + managedEnd + "\n"
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(section), 0o644)
		}
		return err
	}
	s := string(existing)
	start := strings.Index(s, managedBegin)
	end := strings.Index(s, managedEnd)
	if start >= 0 && end > start {
		end += len(managedEnd)
		s = s[:start] + strings.TrimRight(section, "\n") + s[end:]
		return os.WriteFile(path, []byte(s), 0o644)
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return os.WriteFile(path, []byte(s+"\n"+section), 0o644)
}
