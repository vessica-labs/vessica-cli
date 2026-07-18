package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePreviewCommandForVite(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"vite"},"devDependencies":{"vite":"latest"}}`)
	if got := ResolvePreviewCommand(root, "npm run dev", 3000); got != "PORT=3000 pnpm run dev -- --host 0.0.0.0 --port 3000" {
		t.Fatalf("command=%q", got)
	}
}

func TestResolvePreviewCommandForVinextBindsSandboxInterface(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"WRANGLER_LOG_PATH=.wrangler/wrangler.log vinext dev"},"devDependencies":{"vinext":"latest"}}`)
	if got := ResolvePreviewCommand(root, "PORT=3000 pnpm run dev", 3000); got != "PORT=3000 pnpm run dev --hostname 0.0.0.0 --port 3000" {
		t.Fatalf("command=%q", got)
	}
}

func TestResolvePreviewCommandFallsBackToWatchedNodeServer(t *testing.T) {
	root := writePackage(t, `{"scripts":{"start":"node server.mjs"}}`)
	if got := ResolvePreviewCommand(root, "npm run dev", 3000); got != "PORT=3000 node --watch-path=. server.mjs" {
		t.Fatalf("command=%q", got)
	}
}

func TestPreviewInstallCommandUsesPnpmLockfile(t *testing.T) {
	root := writePackage(t, `{"devDependencies":{"vite":"latest"}}`)
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PreviewInstallCommand(root); got != `mkdir -p "$HOME/.local/bin" && corepack enable --install-directory "$HOME/.local/bin" && corepack prepare pnpm@11.9.0 --activate && export PATH="$HOME/.local/bin:$PATH" && pnpm install --frozen-lockfile` {
		t.Fatalf("command=%q", got)
	}
}

func TestResolveNodeCommandTranslatesLegacyNpm(t *testing.T) {
	root := writePackage(t, `{"scripts":{"build":"vite build"}}`)
	if got := ResolveNodeCommand(root, "npm run build && npx vite inspect"); got != "pnpm run build && pnpm exec vite inspect" {
		t.Fatalf("command=%q", got)
	}
}

func TestResolveNodeCommandLeavesPnpmUnchanged(t *testing.T) {
	root := writePackage(t, `{"scripts":{"test":"node --test","build":"vite build"}}`)
	command := "pnpm test && pnpm run build"
	if got := ResolveNodeCommand(root, command); got != command {
		t.Fatalf("command=%q", got)
	}
}

func TestDetectGeneratesPnpmCommands(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"vite","build":"vite build","test":"vitest","lint":"eslint ."}}`)
	detected := Detect(root)
	for _, command := range []string{detected.PreviewCommand, detected.BuildCommand, detected.TestCommand, detected.LintCommand} {
		if !strings.Contains(command, "pnpm") {
			t.Fatalf("non-pnpm command: %q", command)
		}
	}
}

func writePackage(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
