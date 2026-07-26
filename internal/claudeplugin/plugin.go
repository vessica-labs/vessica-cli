package claudeplugin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/codexplugin"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

const pluginID = "vessica@vessica"

//go:embed manifest.json marketplace.json all:assets
var metadata embed.FS

type installResult struct {
	Name            string `json:"name"`
	Path            string `json:"path"`
	Marketplace     string `json:"marketplace"`
	PluginID        string `json:"plugin_id"`
	Version         string `json:"version"`
	Prepared        bool   `json:"prepared"`
	Installed       bool   `json:"installed"`
	ClaudeAvailable bool   `json:"claude_available"`
	InstallCommand  string `json:"install_command,omitempty"`
}

func Install() (*installResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cliVersion := strings.TrimSpace(version.Version)
	if cliVersion == "" || cliVersion == "dev" {
		return nil, fmt.Errorf("install Claude plugin: released Vessica CLI version is unavailable")
	}

	marketplaceRoot := filepath.Join(home, ".vessica", "claude-marketplace")
	pluginVersion := cliVersion + "+claude." + time.Now().UTC().Format("20060102150405.000000000")
	pluginPath, err := RenderMarketplace(marketplaceRoot, cliVersion, pluginVersion)
	if err != nil {
		return nil, err
	}

	result := &installResult{
		Name:            "vessica",
		Path:            pluginPath,
		Marketplace:     marketplaceRoot,
		PluginID:        pluginID,
		Version:         pluginVersion,
		Prepared:        true,
		InstallCommand:  "claude plugin marketplace add " + strconv.Quote(marketplaceRoot) + " --scope user && claude plugin install " + pluginID + " --scope user",
		ClaudeAvailable: false,
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return result, nil
	}
	result.ClaudeAvailable = true
	if err := syncClaudePlugin(claudePath, marketplaceRoot); err != nil {
		return nil, err
	}
	result.Installed = true
	result.InstallCommand = ""
	return result, nil
}

func Status() map[string]any {
	home, _ := os.UserHomeDir()
	marketplaceRoot := filepath.Join(home, ".vessica", "claude-marketplace")
	manifest := filepath.Join(marketplaceRoot, "plugins", "vessica", ".claude-plugin", "plugin.json")
	_, manifestErr := os.Stat(manifest)
	status := map[string]any{
		"plugin":           "vessica",
		"plugin_id":        pluginID,
		"prepared":         manifestErr == nil,
		"installed":        false,
		"manifest":         manifest,
		"marketplace":      marketplaceRoot,
		"claude_available": false,
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return status
	}
	status["claude_available"] = true
	installed, err := installedClaudePlugin(claudePath)
	if err != nil {
		status["status_error"] = err.Error()
		return status
	}
	status["installed"] = installed
	return status
}

// RenderMarketplace writes a self-contained Claude Code marketplace containing
// the Vessica plugin. Release packaging uses the same renderer as local setup so
// the published archive and installed plugin cannot drift.
func RenderMarketplace(marketplaceRoot, cliVersion, pluginVersion string) (string, error) {
	if strings.TrimSpace(cliVersion) == "" || strings.TrimSpace(pluginVersion) == "" {
		return "", fmt.Errorf("render Claude plugin: CLI and plugin versions are required")
	}
	pluginPath := filepath.Join(marketplaceRoot, "plugins", "vessica")
	if err := os.RemoveAll(pluginPath); err != nil {
		return "", fmt.Errorf("clear prior Claude plugin bundle: %w", err)
	}
	if err := writePluginBundle(pluginPath); err != nil {
		return "", err
	}
	if err := writeClaudeOnlyAssets(pluginPath); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(pluginPath, "scripts", "cli-version.txt"), []byte(cliVersion+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("write Claude plugin CLI pin: %w", err)
	}
	if err := writeManifest(filepath.Join(pluginPath, ".claude-plugin", "plugin.json"), pluginVersion); err != nil {
		return "", err
	}
	marketplacePath := filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json")
	if err := writeEmbeddedFile("marketplace.json", marketplacePath, 0o644); err != nil {
		return "", err
	}
	return pluginPath, nil
}

func writePluginBundle(dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	bundle := codexplugin.BundleFS()
	return fs.WalkDir(bundle, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if path == ".codex-plugin" || strings.HasPrefix(path, ".codex-plugin/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dest, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := fs.ReadFile(bundle, path)
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".md" {
			body = adaptForClaude(body)
		}
		mode := os.FileMode(0o644)
		if filepath.Base(filepath.Dir(target)) == "scripts" || filepath.Ext(target) == ".sh" {
			mode = 0o755
		}
		return os.WriteFile(target, body, mode)
	})
}

func adaptForClaude(body []byte) []byte {
	replacer := strings.NewReplacer(
		"# Vessica for Codex", "# Vessica for Claude Code",
		"This plugin teaches Codex", "This plugin teaches Claude Code",
		"The hosted control plane and `ves` remain authoritative.", "The hosted control plane and `ves` remain authoritative. Claude Code operates Vessica, while production engineering runs continue to use the Codex runner.",
		"Codex local memories, rollout summaries, or session logs", "Claude Code local memory, conversation history, or session logs",
		"ves prime --for codex", "ves prime --for claude",
		"ves setup codex --check --json", "ves setup claude --check --json",
		"Preserve explicit run options when dispatching. Use local execution only", "Preserve explicit run options when dispatching. Do not select Claude as the runner; production execution remains Codex. Use local execution only",
		"- Diagnose hosted state, Railway preview forwarding, knowledge retrieval, and", "- Safely scan Outlook through Claude Desktop's authorized connector and prepare validated, deduplicated Vessica ingestion batches.\n- Diagnose hosted state, Railway preview forwarding, knowledge retrieval, and",
		"../../scripts/ensure-ves.sh", `"${CLAUDE_PLUGIN_ROOT}/scripts/ensure-ves.sh"`,
	)
	return []byte(replacer.Replace(string(body)))
}

func writeClaudeOnlyAssets(dest string) error {
	return fs.WalkDir(metadata, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "assets" {
			return nil
		}
		rel, err := filepath.Rel("assets", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := metadata.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if filepath.Base(filepath.Dir(target)) == "scripts" {
			mode = 0o755
		}
		return os.WriteFile(target, body, mode)
	})
}

func writeManifest(path, pluginVersion string) error {
	raw, err := metadata.ReadFile("manifest.json")
	if err != nil {
		return fmt.Errorf("read Claude plugin manifest: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("decode Claude plugin manifest: %w", err)
	}
	manifest["version"] = pluginVersion
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Claude plugin manifest: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Claude plugin manifest: %w", err)
	}
	return nil
}

func writeEmbeddedFile(source, target string, mode os.FileMode) error {
	body, err := metadata.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read Claude plugin %s: %w", source, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(target, body, mode); err != nil {
		return fmt.Errorf("write Claude plugin %s: %w", source, err)
	}
	return nil
}

func syncClaudePlugin(claudePath, marketplaceRoot string) error {
	marketplaces, err := runClaudeJSON(claudePath, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return err
	}
	if jsonContainsString(marketplaces, "vessica") {
		if _, err := runClaude(claudePath, "plugin", "marketplace", "update", "vessica"); err != nil {
			return err
		}
	} else if _, err := runClaude(claudePath, "plugin", "marketplace", "add", marketplaceRoot, "--scope", "user"); err != nil {
		return err
	}

	installed, err := installedClaudePlugin(claudePath)
	if err != nil {
		return err
	}
	if installed {
		if _, err := runClaude(claudePath, "plugin", "update", pluginID, "--scope", "user"); err != nil {
			return err
		}
	} else if _, err := runClaude(claudePath, "plugin", "install", pluginID, "--scope", "user"); err != nil {
		return err
	}
	installed, err = installedClaudePlugin(claudePath)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("install Claude plugin: %s was not present after installation", pluginID)
	}
	return nil
}

func installedClaudePlugin(claudePath string) (bool, error) {
	plugins, err := runClaudeJSON(claudePath, "plugin", "list", "--json")
	if err != nil {
		return false, err
	}
	return jsonContainsString(plugins, pluginID), nil
}

func runClaudeJSON(claudePath string, args ...string) (any, error) {
	output, err := runClaude(claudePath, args...)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(output, &value); err != nil {
		return nil, fmt.Errorf("decode Claude plugin command output: %w", err)
	}
	return value, nil
}

func runClaude(claudePath string, args ...string) ([]byte, error) {
	cmd := exec.Command(claudePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			return nil, fmt.Errorf("run claude %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("run claude %s: %w: %s", strings.Join(args, " "), err, detail)
	}
	return output, nil
}

func jsonContainsString(value any, target string) bool {
	switch item := value.(type) {
	case string:
		return item == target
	case []any:
		for _, child := range item {
			if jsonContainsString(child, target) {
				return true
			}
		}
	case map[string]any:
		for _, child := range item {
			if jsonContainsString(child, target) {
				return true
			}
		}
	}
	return false
}
