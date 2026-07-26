package claudeplugin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/version"
)

func TestInstallWritesAndRegistersClaudePlugin(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
printf '%s\n' "$*" >> "$HOME/claude-plugin.log"
case "$*" in
  "plugin marketplace list --json")
    if [ -f "$HOME/marketplace-added" ]; then
      printf '[{"name":"vessica"}]'
    else
      printf '[]'
    fi
    ;;
  plugin\ marketplace\ add*)
    : > "$HOME/marketplace-added"
    ;;
  "plugin marketplace update vessica")
    ;;
  "plugin list --json")
    if [ -f "$HOME/plugin-installed" ]; then
      printf '[{"id":"vessica@vessica","version":"test"}]'
    else
      printf '[]'
    fi
    ;;
  "plugin install vessica@vessica --scope user")
    : > "$HOME/plugin-installed"
    ;;
  "plugin update vessica@vessica --scope user")
    ;;
  *)
    printf 'unexpected claude invocation: %s\n' "$*" >&2
    exit 1
    ;;
esac
`)
	t.Setenv("HOME", home)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+"/usr/bin:/bin")

	result, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Prepared || !result.Installed || !result.ClaudeAvailable {
		t.Fatalf("result=%#v", result)
	}
	if result.PluginID != pluginID || result.InstallCommand != "" {
		t.Fatalf("result=%#v", result)
	}
	assertGeneratedBundle(t, result.Path, result.Marketplace)

	if _, err := Install(); err != nil {
		t.Fatal(err)
	}
	logBody, err := os.ReadFile(filepath.Join(home, "claude-plugin.log"))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logBody)
	for _, want := range []string{
		"plugin marketplace add " + result.Marketplace + " --scope user",
		"plugin install vessica@vessica --scope user",
		"plugin marketplace update vessica",
		"plugin update vessica@vessica --scope user",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("Claude command log does not contain %q:\n%s", want, logText)
		}
	}
}

func TestInstallPreparesPluginWhenClaudeIsUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty-path"))

	result, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Prepared || result.Installed || result.ClaudeAvailable {
		t.Fatalf("result=%#v", result)
	}
	if !strings.Contains(result.InstallCommand, "claude plugin marketplace add") || !strings.Contains(result.InstallCommand, pluginID) {
		t.Fatalf("install command=%q", result.InstallCommand)
	}
	assertGeneratedBundle(t, result.Path, result.Marketplace)
}

func TestGeneratedPluginPassesClaudeValidation(t *testing.T) {
	if os.Getenv("TEST_CLAUDE_PLUGIN_VALIDATION") != "1" {
		t.Skip("set TEST_CLAUDE_PLUGIN_VALIDATION=1 to run Claude Code validation")
	}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("Claude Code is not installed")
	}
	marketplaceRoot := t.TempDir()
	pluginPath, err := RenderMarketplace(marketplaceRoot, version.Version, version.Version+"+claude.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{pluginPath, marketplaceRoot} {
		cmd := exec.Command(claudePath, "plugin", "validate", path, "--strict")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("claude plugin validate %s: %v\n%s", path, err, output)
		}
	}
}

func TestPackagedOutlookIngestionValidatorAcceptsEmptyBatch(t *testing.T) {
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	marketplaceRoot := t.TempDir()
	pluginPath, err := RenderMarketplace(marketplaceRoot, version.Version, version.Version+"+claude.test")
	if err != nil {
		t.Fatal(err)
	}
	batchPath := filepath.Join(t.TempDir(), "batch.json")
	batch := `{
  "schema": "vessica.email-ingestion/v1",
  "generated_at": "2026-07-25T07:30:01-07:00",
  "source": {"surface": "claude_desktop", "connector": "outlook"},
  "window": {
    "start": "2026-07-25T07:00:00-07:00",
    "end": "2026-07-25T07:30:00-07:00",
    "timezone": "America/Los_Angeles"
  },
  "checkpoint": {
    "previous": null,
    "candidate": "2026-07-25T07:30:00-07:00"
  },
  "messages": [],
  "contact_updates": [],
  "batch_summary": {
    "messages_scanned": 0,
    "messages_included": 0,
    "response_required": 0,
    "fyi": 0,
    "contact_updates": 0,
    "warnings": []
  }
}`
	if err := os.WriteFile(batchPath, []byte(batch), 0o600); err != nil {
		t.Fatal(err)
	}
	validator := filepath.Join(pluginPath, "skills", "vessica-outlook-ingestion", "scripts", "validate_ingestion.py")
	cmd := exec.Command(pythonPath, validator, batchPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("packaged Outlook validator failed: %v\n%s", err, output)
	}
}

func assertGeneratedBundle(t *testing.T, pluginPath, marketplaceRoot string) {
	t.Helper()
	manifestRaw, err := os.ReadFile(filepath.Join(pluginPath, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Skills  string `json:"skills"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "vessica" || manifest.Skills != "./skills/" || !strings.HasPrefix(manifest.Version, version.Version+"+claude.") {
		t.Fatalf("manifest=%s", manifestRaw)
	}
	if _, err := os.Stat(filepath.Join(pluginPath, ".codex-plugin")); !os.IsNotExist(err) {
		t.Fatalf("Claude bundle contains Codex manifest: %v", err)
	}
	setupBody, err := os.ReadFile(filepath.Join(pluginPath, "skills", "setup-vessica", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(setupBody), `"${CLAUDE_PLUGIN_ROOT}/scripts/ensure-ves.sh"`) || strings.Contains(string(setupBody), "../../scripts/ensure-ves.sh") {
		t.Fatalf("setup skill did not use the Claude plugin root:\n%s", setupBody)
	}
	workBody, err := os.ReadFile(filepath.Join(pluginPath, "skills", "work-with-vessica", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workBody), "ves prime --for claude") || strings.Contains(string(workBody), "ves prime --for codex") {
		t.Fatalf("workflow skill did not target Claude:\n%s", workBody)
	}
	knowledgeBody, err := os.ReadFile(filepath.Join(pluginPath, "skills", "use-knowledge", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(knowledgeBody), "Claude Code local memory") || strings.Contains(string(knowledgeBody), "Codex local memories") {
		t.Fatalf("knowledge skill did not target Claude:\n%s", knowledgeBody)
	}
	operatorBody, err := os.ReadFile(filepath.Join(pluginPath, "skills", "operate-vessica", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(operatorBody), "ves setup claude --check --json") {
		t.Fatalf("operator skill did not target Claude:\n%s", operatorBody)
	}
	dispatchBody, err := os.ReadFile(filepath.Join(pluginPath, "skills", "dispatch-epic", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dispatchBody), "Do not select Claude as the runner; production execution remains Codex") {
		t.Fatalf("dispatch skill did not preserve the Codex runner boundary:\n%s", dispatchBody)
	}
	readmeBody, err := os.ReadFile(filepath.Join(pluginPath, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readmeBody), "production engineering runs continue to use the Codex runner") {
		t.Fatalf("README did not preserve the Codex runner boundary:\n%s", readmeBody)
	}
	outlookSkill := filepath.Join(pluginPath, "skills", "vessica-outlook-ingestion")
	for _, relativePath := range []string{
		"SKILL.md",
		"references/account-scope.md",
		"references/ingestion-contract.md",
		"scripts/validate_ingestion.py",
	} {
		if _, err := os.Stat(filepath.Join(outlookSkill, relativePath)); err != nil {
			t.Fatalf("packaged Outlook skill is missing %s: %v", relativePath, err)
		}
	}
	for _, excludedPath := range []string{".DS_Store", "scripts/package_claude_skill.py"} {
		if _, err := os.Stat(filepath.Join(outlookSkill, excludedPath)); !os.IsNotExist(err) {
			t.Fatalf("packaged Outlook skill contains excluded file %s: %v", excludedPath, err)
		}
	}
	bootstrap, err := os.Stat(filepath.Join(pluginPath, "scripts", "ensure-ves.sh"))
	if err != nil || bootstrap.Mode()&0o111 == 0 {
		t.Fatalf("bootstrap is not executable: info=%v err=%v", bootstrap, err)
	}
	pin, err := os.ReadFile(filepath.Join(pluginPath, "scripts", "cli-version.txt"))
	if err != nil || strings.TrimSpace(string(pin)) != version.Version {
		t.Fatalf("CLI pin=%q err=%v", pin, err)
	}
	marketplaceRaw, err := os.ReadFile(filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(marketplaceRaw), `"source": "./plugins/vessica"`) {
		t.Fatalf("marketplace=%s", marketplaceRaw)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
