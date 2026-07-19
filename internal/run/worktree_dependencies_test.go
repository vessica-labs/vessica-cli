package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyContractChangedAfterTicketUpdatesLockfile(t *testing.T) {
	root := t.TempDir()
	workdir := t.TempDir()
	for _, dir := range []string{root, workdir} {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"yarn@4.9.2"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte("base\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := dependencyContractChanged(root, workdir)
	if err != nil || changed {
		t.Fatalf("initial changed=%v err=%v", changed, err)
	}
	if err := os.WriteFile(filepath.Join(workdir, "yarn.lock"), []byte("ticket update\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = dependencyContractChanged(root, workdir)
	if err != nil || !changed {
		t.Fatalf("updated changed=%v err=%v", changed, err)
	}
}

func TestDependencyContractChangedIncludesNestedPythonManifest(t *testing.T) {
	root := t.TempDir()
	workdir := t.TempDir()
	for _, dir := range []string{root, workdir} {
		if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "api", "requirements.txt"), []byte("fastapi==1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workdir, "api", "requirements.txt"), []byte("fastapi==2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := dependencyContractChanged(root, workdir)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}
