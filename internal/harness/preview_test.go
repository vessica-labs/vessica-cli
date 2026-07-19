package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePreviewCommandForVite(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"vite"},"devDependencies":{"vite":"latest"}}`)
	if got := ResolvePreviewCommand(root, "npm run dev", 3000); got != "PORT=3000 npm run dev -- --host 0.0.0.0 --port 3000" {
		t.Fatalf("command=%q", got)
	}
}

func TestResolvePreviewCommandForViteIsIdempotent(t *testing.T) {
	root := writePackage(t, `{"packageManager":"yarn@4.9.2","scripts":{"dev":"vite dev"},"devDependencies":{"vite":"latest"}}`)
	configured := "PORT=3000 corepack yarn run dev -- --host 0.0.0.0 --port 3000"
	if got := ResolvePreviewCommand(root, configured, 3000); got != configured {
		t.Fatalf("command=%q", got)
	}
}

func TestResolvePreviewCommandForVinextBindsSandboxInterface(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"WRANGLER_LOG_PATH=.wrangler/wrangler.log vinext dev"},"devDependencies":{"vinext":"latest"}}`)
	if got := ResolvePreviewCommand(root, "PORT=3000 pnpm run dev", 3000); got != "PORT=3000 npm run dev --hostname 0.0.0.0 --port 3000" {
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

func TestResolveNodeCommandKeepsAuthoritativeNpm(t *testing.T) {
	root := writePackage(t, `{"scripts":{"build":"vite build"}}`)
	if got := ResolveNodeCommand(root, "npm run build && npx vite inspect"); got != "npm run build && npx vite inspect" {
		t.Fatalf("command=%q", got)
	}
}

func TestResolveNodeCommandLeavesPnpmUnchanged(t *testing.T) {
	root := writePackage(t, `{"scripts":{"test":"node --test","build":"vite build"}}`)
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := "pnpm test && pnpm run build"
	if got := ResolveNodeCommand(root, command); got != command {
		t.Fatalf("command=%q", got)
	}
}

func TestDetectGeneratesAuthoritativeNpmCommands(t *testing.T) {
	root := writePackage(t, `{"scripts":{"dev":"vite","build":"vite build","test":"vitest","lint":"eslint ."}}`)
	detected := Detect(root)
	for _, command := range []string{detected.PreviewCommand, detected.BuildCommand, detected.TestCommand, detected.LintCommand} {
		if !strings.Contains(command, "npm") {
			t.Fatalf("non-npm command: %q", command)
		}
	}
}

func TestDetectCompositeStackAndPackageManagerField(t *testing.T) {
	root := writePackage(t, `{"packageManager":"yarn@4.2.0","scripts":{"build":"vite build"}}`)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detected := Detect(root)
	if detected.Stack != "node+go" || detected.PackageManager != "yarn" || detected.BuildCommand != "corepack yarn run build" {
		t.Fatalf("detected=%#v", detected)
	}
}

func TestDetectGeneratesDjangoHarnessCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("Django==4.2.30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manage.py"), []byte("# django entrypoint\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	detected := Detect(root)
	if detected.Stack != "python" || !strings.Contains(detected.PreviewCommand, "manage.py runserver") || !strings.Contains(detected.TestCommand, "manage.py test") || !strings.Contains(detected.LintCommand, "makemigrations --check") {
		t.Fatalf("detected=%#v", detected)
	}
}

func TestDetectGeneratesFastAPIHarnessCommands(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("fastapi\nuvicorn\npytest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "main.py"), []byte("app = FastAPI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	detected := Detect(root)
	if !strings.Contains(detected.PreviewCommand, "uvicorn app.main:app") || !strings.Contains(detected.TestCommand, "pytest") {
		t.Fatalf("detected=%#v", detected)
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
