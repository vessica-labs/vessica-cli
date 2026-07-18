package reposnapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointNameAndMetadataRoundTrip(t *testing.T) {
	checkpoint := Checkpoint{SchemaVersion: SchemaVersion, Name: Name("github.com/acme/demo", strings.Repeat("a", 40), strings.Repeat("b", 64), "toolchain123"), Status: "ready", ToolchainFingerprint: "toolchain123"}
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
