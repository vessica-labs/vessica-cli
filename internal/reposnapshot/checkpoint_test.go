package reposnapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointNameAndMetadataRoundTrip(t *testing.T) {
	checkpoint := Checkpoint{SchemaVersion: SchemaVersion, Name: Name("github.com/acme/demo", strings.Repeat("a", 40), strings.Repeat("b", 64), "spec123", "toolchain123"), Status: "ready", ToolchainFingerprint: "toolchain123", VerifiedAt: "2026-07-18T00:00:00Z"}
	if len(checkpoint.Name) > 64 || !strings.HasPrefix(checkpoint.Name, "vessica-repo-") {
		t.Fatalf("name=%q", checkpoint.Name)
	}
	raw, err := Merge(`{"owner":"acme"}`, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	parsed, ok := Parse(raw)
	if !ok || parsed.Name != checkpoint.Name || !parsed.Ready("toolchain123") || !strings.Contains(raw, `"owner":"acme"`) {
		t.Fatalf("raw=%s parsed=%#v ok=%t", raw, parsed, ok)
	}
	if parsed.Ready("different") {
		t.Fatal("snapshot must be invalidated by a toolchain change")
	}
}

func TestDependencyInstallPlanIncludesNestedCompositeStacks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	web := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package.json"), []byte(`{"name":"web"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, command := DependencyInstallCommand(root)
	if stack != "go+node" || !strings.Contains(command, "go mod download") || !strings.Contains(command, "cd 'apps/web' && npm ci") {
		t.Fatalf("stack=%q command=%q", stack, command)
	}
	spec, _ := InferSpecification([]string{"go.mod", "apps/web/package.json", "apps/web/package-lock.json"}, stack)
	if len(spec.Environments) != 2 || len(spec.PackageManagers) != 2 || len(spec.WorkspaceRoots) != 2 {
		t.Fatalf("spec=%#v", spec)
	}
}

func TestDependencyFingerprintIncludesHarnessContract(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vessica"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vessica", "harness.yaml"), []byte("build: {command: npm run build}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, _ := DependencyFingerprint(root)
	if err := os.WriteFile(filepath.Join(root, ".vessica", "harness.yaml"), []byte("build: {command: npm run compile}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, _ := DependencyFingerprint(root)
	if first == second {
		t.Fatal("harness change did not invalidate environment fingerprint")
	}
}

func TestDependencyFingerprintIgnoresSourceChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := DependencyFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "app.ts"), []byte("export {}"), 0o644)
	second, _ := DependencyFingerprint(root)
	if first != second {
		t.Fatal("source-only change invalidated dependencies")
	}
	_ = os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: 9"), 0o644)
	third, _ := DependencyFingerprint(root)
	if third == second {
		t.Fatal("lockfile change did not invalidate dependencies")
	}
	stack, command := DependencyInstallCommand(root)
	if stack != "node" || !strings.Contains(command, "frozen-lockfile") {
		t.Fatalf("stack=%s command=%s", stack, command)
	}
}

func TestInferSpecificationCapturesPurposeBuiltNodeContract(t *testing.T) {
	spec, fingerprint := InferSpecification([]string{"package.json", "pnpm-lock.yaml", "app/page.tsx"}, "node")
	if spec.PackageManager != "pnpm" || len(spec.Manifests) != 2 || len(spec.RequiredTools) != 2 || fingerprint == "" {
		t.Fatalf("spec=%#v fingerprint=%q", spec, fingerprint)
	}
}
