package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"gopkg.in/yaml.v3"
)

var DocFiles = []string{
	"AGENTS.md",
	"ARCHITECTURE.md",
	"DESIGN.md",
	"DEPLOY.md",
	"TESTING.md",
	"SECURITY.md",
}

const PnpmVersion = "11.9.0"

func PnpmBootstrapCommand() string {
	// Hosted repository commands run as an unprivileged user and cannot create
	// Corepack shims in /usr/local/bin. Install the shims in the runner's home
	// instead, then put that directory first for the rest of the shell chain.
	return `mkdir -p "$HOME/.local/bin" && corepack enable --install-directory "$HOME/.local/bin" && corepack prepare pnpm@` + PnpmVersion + ` --activate && export PATH="$HOME/.local/bin:$PATH"`
}

type HarnessYAML struct {
	Preview Preview    `yaml:"preview" json:"preview"`
	Build   Build      `yaml:"build" json:"build"`
	Test    Test       `yaml:"test" json:"test"`
	Lint    LintConfig `yaml:"lint" json:"lint"`
}

type Preview struct {
	Command     string `yaml:"command" json:"command"`
	Port        int    `yaml:"port" json:"port"`
	Healthcheck string `yaml:"healthcheck" json:"healthcheck"`
}

type Build struct {
	Command string `yaml:"command" json:"command"`
}

type Test struct {
	Command string `yaml:"command" json:"command"`
}

type LintConfig struct {
	Command string `yaml:"command" json:"command"`
	Arch    string `yaml:"arch" json:"arch"`
}

type Status struct {
	LastSyncAt   string   `json:"last_sync_at,omitempty"`
	DriftStatus  string   `json:"drift_status"`
	PackVersion  string   `json:"pack_version,omitempty"`
	MissingFiles []string `json:"missing_files"`
	PresentFiles []string `json:"present_files"`
}

type AuditResult struct {
	Missing []string `json:"missing"`
	Present []string `json:"present"`
	Drift   string   `json:"drift"`
}

func yamlPath(root string) string {
	return filepath.Join(root, config.DirName, config.HarnessFile)
}

func Create(root string) (*Status, error) {
	return Sync(root)
}

func Audit(root string) (*AuditResult, error) {
	var missing, present []string
	for _, f := range DocFiles {
		p := filepath.Join(root, f)
		if fileExists(p) {
			present = append(present, f)
		} else {
			missing = append(missing, f)
		}
	}
	if !fileExists(yamlPath(root)) {
		missing = append(missing, ".vessica/harness.yaml")
	} else {
		present = append(present, ".vessica/harness.yaml")
	}
	drift := "ok"
	if len(missing) > 0 {
		drift = "missing_files"
	}
	return &AuditResult{Missing: missing, Present: present, Drift: drift}, nil
}

func Sync(root string) (*Status, error) {
	detect := Detect(root)
	hy := DetectConfig(root)
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		return nil, err
	}
	b, err := yaml.Marshal(hy)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(yamlPath(root), b, 0o644); err != nil {
		return nil, err
	}

	for _, name := range DocFiles {
		p := filepath.Join(root, name)
		if fileExists(p) {
			continue
		}
		content := defaultDoc(name, detect)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return nil, err
		}
	}

	lintArch := filepath.Join(root, config.DirName, "lint-arch.sh")
	if !fileExists(lintArch) {
		script := "#!/usr/bin/env bash\nset -euo pipefail\necho \"lint-arch: ok\"\n"
		_ = os.WriteFile(lintArch, []byte(script), 0o755)
	}

	st, err := StatusOf(root)
	if err != nil {
		return nil, err
	}
	st.LastSyncAt = time.Now().UTC().Format(time.RFC3339)
	st.DriftStatus = "ok"
	_ = writeStatusMarker(root, st.LastSyncAt)
	return st, nil
}

func Lint(root string) (map[string]any, error) {
	hy, err := Load(root)
	if err != nil {
		return nil, err
	}
	results := []map[string]any{}
	ok := true
	// Deterministic checks
	for _, f := range DocFiles {
		exists := fileExists(filepath.Join(root, f))
		results = append(results, map[string]any{"check": "file:" + f, "ok": exists})
		if !exists {
			ok = false
		}
	}
	if hy.Preview.Port <= 0 {
		ok = false
		results = append(results, map[string]any{"check": "preview.port", "ok": false})
	} else {
		results = append(results, map[string]any{"check": "preview.port", "ok": true})
	}
	return map[string]any{"ok": ok, "results": results}, nil
}

func StatusOf(root string) (*Status, error) {
	audit, err := Audit(root)
	if err != nil {
		return nil, err
	}
	st := &Status{
		DriftStatus:  audit.Drift,
		MissingFiles: audit.Missing,
		PresentFiles: audit.Present,
	}
	if b, err := os.ReadFile(filepath.Join(root, config.DirName, "harness.sync")); err == nil {
		st.LastSyncAt = strings.TrimSpace(string(b))
	}
	if lockb, err := os.ReadFile(filepath.Join(root, config.DirName, config.PackLockFile)); err == nil {
		st.PackVersion = string(lockb)
	}
	return st, nil
}

func Load(root string) (*HarnessYAML, error) {
	b, err := os.ReadFile(yamlPath(root))
	if err != nil {
		return nil, fmt.Errorf("harness.yaml missing; run ves harness sync")
	}
	var hy HarnessYAML
	if err := yaml.Unmarshal(b, &hy); err != nil {
		return nil, err
	}
	return &hy, nil
}

type Detected struct {
	PreviewCommand string
	PreviewPort    int
	Healthcheck    string
	BuildCommand   string
	TestCommand    string
	LintCommand    string
	Stack          string
}

// DetectConfig returns a usable harness without writing repository files.
func DetectConfig(root string) HarnessYAML {
	detect := Detect(root)
	return HarnessYAML{
		Preview: Preview{
			Command:     detect.PreviewCommand,
			Port:        detect.PreviewPort,
			Healthcheck: detect.Healthcheck,
		},
		Build: Build{Command: detect.BuildCommand},
		Test:  Test{Command: detect.TestCommand},
		Lint:  LintConfig{Command: detect.LintCommand, Arch: ".vessica/lint-arch.sh"},
	}
}

func Detect(root string) Detected {
	d := Detected{
		PreviewPort: 3000,
		Healthcheck: "http://localhost:3000/",
		Stack:       "generic",
	}
	switch {
	case fileExists(filepath.Join(root, "package.json")):
		d.Stack = "node"
		scripts := packageScripts(root)
		d.PreviewCommand = nodePreviewCommand(root, scripts, "", d.PreviewPort)
		if d.PreviewCommand == "" {
			d.PreviewCommand = "echo 'configure preview.command in .vessica/harness.yaml'"
		}
		d.BuildCommand = nodeScriptCommand(scripts, "build")
		d.TestCommand = nodeScriptCommand(scripts, "test")
		d.LintCommand = nodeScriptCommand(scripts, "lint")
		if fileContains(filepath.Join(root, "server.mjs"), "/health") || fileContains(filepath.Join(root, "server.js"), "/health") {
			d.Healthcheck = "http://localhost:3000/health"
		}
	case fileExists(filepath.Join(root, "go.mod")):
		d.Stack = "go"
		d.PreviewCommand = goPreviewCommand(root)
		d.PreviewPort = 8080
		d.Healthcheck = "http://localhost:8080/"
		d.BuildCommand = "go build ./..."
		d.TestCommand = "go test ./..."
		d.LintCommand = "go vet ./..."
	case fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "requirements.txt")):
		d.Stack = "python"
		d.PreviewPort = 8000
		d.Healthcheck = "http://localhost:8000/"
		d.BuildCommand = "python -m compileall ."
	}
	return d
}

func goPreviewCommand(root string) string {
	if fileContains(filepath.Join(root, "main.go"), "package main") {
		return "go run ."
	}
	entries, _ := filepath.Glob(filepath.Join(root, "cmd", "*", "main.go"))
	var commands []string
	for _, entry := range entries {
		if fileContains(entry, "package main") {
			commands = append(commands, "go run ./cmd/"+filepath.Base(filepath.Dir(entry)))
		}
	}
	if len(commands) == 1 {
		return commands[0]
	}
	return ""
}

func packageScripts(root string) map[string]string {
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

func nodeScriptCommand(scripts map[string]string, name string) string {
	if scripts == nil {
		return ""
	}
	if strings.TrimSpace(scripts[name]) == "" {
		return ""
	}
	return "pnpm run " + name
}

// ResolveNodeCommand translates shell command tokens from npm to pnpm.
func ResolveNodeCommand(root, configured string) string {
	configured = strings.TrimSpace(configured)
	if !fileExists(filepath.Join(root, "package.json")) {
		return configured
	}
	configured = replaceShellPhrase(configured, "npm ci", "pnpm install --frozen-lockfile")
	configured = replaceShellPhrase(configured, "npm install", "pnpm install --no-lockfile")
	configured = replaceShellPhrase(configured, "npm", "pnpm")
	return replaceShellPhrase(configured, "npx", "pnpm exec")
}

func replaceShellPhrase(command, legacy, replacement string) string {
	var out strings.Builder
	for cursor := 0; cursor < len(command); {
		relative := strings.Index(command[cursor:], legacy)
		if relative < 0 {
			out.WriteString(command[cursor:])
			break
		}
		start := cursor + relative
		end := start + len(legacy)
		if shellBoundary(command, start-1) && shellBoundary(command, end) {
			out.WriteString(command[cursor:start])
			out.WriteString(replacement)
			cursor = end
			continue
		}
		out.WriteString(command[cursor:end])
		cursor = end
	}
	return out.String()
}

func shellBoundary(command string, index int) bool {
	if index < 0 || index >= len(command) {
		return true
	}
	switch command[index] {
	case ' ', '\t', '\r', '\n', ';', '&', '|', '(', ')':
		return true
	default:
		return false
	}
}

// ResolvePreviewCommand repairs generated Node preview commands when scripts
// changed and applies development-friendly host binding and watch behavior.
func ResolvePreviewCommand(root, configured string, port int) string {
	configured = ResolveNodeCommand(root, configured)
	if !fileExists(filepath.Join(root, "package.json")) {
		return configured
	}
	return nodePreviewCommand(root, packageScripts(root), configured, port)
}

// PreviewInstallCommand returns a lockfile-aware dependency bootstrap command.
// It avoids creating a new lockfile in the integration checkout.
func PreviewInstallCommand(root string) string {
	if !fileExists(filepath.Join(root, "package.json")) {
		return ""
	}
	var pkg struct {
		Dependencies    map[string]any `json:"dependencies"`
		DevDependencies map[string]any `json:"devDependencies"`
	}
	b, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil || json.Unmarshal(b, &pkg) != nil || len(pkg.Dependencies)+len(pkg.DevDependencies) == 0 {
		return ""
	}
	switch {
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		return PnpmBootstrapCommand() + " && pnpm install --frozen-lockfile"
	default:
		return PnpmBootstrapCommand() + " && pnpm install --no-lockfile"
	}
}

func nodePreviewCommand(root string, scripts map[string]string, configured string, port int) string {
	name := configuredNodeScript(configured)
	if name != "" && strings.TrimSpace(scripts[name]) == "" {
		configured = ""
		name = ""
	}
	if configured == "" {
		if strings.TrimSpace(scripts["dev"]) != "" {
			name = "dev"
		} else if strings.TrimSpace(scripts["start"]) != "" {
			name = "start"
		} else {
			return ""
		}
	}
	if name == "" {
		return configured
	}
	script := strings.TrimSpace(scripts[name])
	portEnv := ""
	if port > 0 {
		portEnv = fmt.Sprintf("PORT=%d ", port)
	}
	if strings.Contains(script, "vinext") {
		command := portEnv + "pnpm run " + name
		if !strings.Contains(script, "--hostname") {
			command += " --hostname 0.0.0.0"
		}
		if port > 0 && !strings.Contains(script, "--port") {
			command += fmt.Sprintf(" --port %d", port)
		}
		return command
	}
	if strings.Contains(script, "vite") {
		command := portEnv + "pnpm run " + name + " --"
		if !strings.Contains(script, "--host") {
			command += " --host 0.0.0.0"
		}
		if port > 0 && !strings.Contains(script, "--port") {
			command += fmt.Sprintf(" --port %d", port)
		}
		return command
	}
	if strings.HasPrefix(script, "node ") && !strings.Contains(script, "--watch") {
		return portEnv + "node --watch-path=. " + strings.TrimSpace(strings.TrimPrefix(script, "node "))
	}
	return portEnv + "pnpm run " + name
}

func configuredNodeScript(command string) string {
	fields := strings.Fields(command)
	offset := 0
	if len(fields) > 0 && strings.HasPrefix(fields[0], "PORT=") {
		offset = 1
	}
	if len(fields) > offset+2 && (fields[offset] == "npm" || fields[offset] == "pnpm") && fields[offset+1] == "run" {
		return fields[offset+2]
	}
	if len(fields) > offset+1 && fields[offset] == "node" && strings.HasPrefix(fields[offset+1], "--watch") {
		return "start"
	}
	return ""
}

func writeStatusMarker(root, ts string) error {
	return os.WriteFile(filepath.Join(root, config.DirName, "harness.sync"), []byte(ts+"\n"), 0o644)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func fileContains(p, needle string) bool {
	b, err := os.ReadFile(p)
	return err == nil && strings.Contains(string(b), needle)
}
