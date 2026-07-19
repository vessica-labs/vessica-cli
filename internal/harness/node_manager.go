package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// NodePackageManager returns the repository's authoritative package manager.
// The packageManager field wins over lockfile inference; npm is the safe
// default because it ships with Node and does not invent a pnpm contract.
func NodePackageManager(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var pkg struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			if name := strings.SplitN(strings.TrimSpace(pkg.PackageManager), "@", 2)[0]; name == "npm" || name == "pnpm" || name == "yarn" {
				return name
			}
		}
	}
	switch {
	case fileExists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm"
	case fileExists(filepath.Join(root, "yarn.lock")):
		return "yarn"
	default:
		return "npm"
	}
}

func nodeRunCommand(root, name string) string {
	switch NodePackageManager(root) {
	case "pnpm":
		return "pnpm run " + name
	case "yarn":
		return "corepack yarn run " + name
	default:
		return "npm run " + name
	}
}

// ResolveNodeCommand translates generated package-manager tokens to the
// repository's authoritative package manager.
func ResolveNodeCommand(root, configured string) string {
	configured = strings.TrimSpace(configured)
	if !fileExists(filepath.Join(root, "package.json")) {
		return configured
	}
	switch NodePackageManager(root) {
	case "pnpm":
		configured = replaceShellPhrase(configured, "npm ci", "pnpm install --frozen-lockfile")
		configured = replaceShellPhrase(configured, "npm install", "pnpm install --no-lockfile")
		configured = replaceShellPhrase(configured, "npm", "pnpm")
		return replaceShellPhrase(configured, "npx", "pnpm exec")
	case "yarn":
		configured = replaceShellPhrase(configured, "pnpm run", "corepack yarn run")
		return replaceShellPhrase(configured, "npm run", "corepack yarn run")
	default:
		configured = replaceShellPhrase(configured, "pnpm run", "npm run")
		return replaceShellPhrase(configured, "pnpm exec", "npx")
	}
}

func replaceShellPhrase(command, legacy, replacement string) string {
	var out strings.Builder
	for cursor := 0; cursor < len(command); {
		relative := strings.Index(command[cursor:], legacy)
		if relative < 0 {
			out.WriteString(command[cursor:])
			break
		}
		start, end := cursor+relative, cursor+relative+len(legacy)
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
	case fileExists(filepath.Join(root, "package-lock.json")):
		return "npm ci"
	case fileExists(filepath.Join(root, "yarn.lock")):
		return "corepack yarn install --immutable"
	default:
		return "npm install --no-package-lock"
	}
}
