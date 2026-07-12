package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadSet(t *testing.T) {
	dir := t.TempDir()
	c := Defaults()
	c.Runner.Default = "codex"
	if err := Save(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Runner.Default != "codex" {
		t.Fatal(got.Runner.Default)
	}
	if got.Runner.Model != "gpt-5.6-terra" || got.Runner.ReasoningEffort != "high" {
		t.Fatalf("runner defaults = %#v", got.Runner)
	}
	if err := Set(&got, "repo.remote", "git@github.com:o/r.git"); err != nil {
		t.Fatal(err)
	}
	v, err := Get(got, "repo.remote")
	if err != nil || v != "git@github.com:o/r.git" {
		t.Fatalf("%v %v", v, err)
	}
	root, err := FindRoot(filepath.Join(dir, "sub"))
	if err == nil {
		_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
		root, err = FindRoot(dir)
	}
	if err != nil || root != dir {
		// FindRoot from dir itself
		root, err = FindRoot(dir)
		if err != nil || root != dir {
			t.Fatalf("root=%s err=%v", root, err)
		}
	}
}

func TestApplyEnvLoadsRailwayWorkerCheckpoint(t *testing.T) {
	t.Setenv("VES_RAILWAY_CHECKPOINT", "vessica-worker-test")
	cfg := TeamDefaults()
	ApplyEnv(&cfg)
	if cfg.Hosted.WorkerCheckpoint != "vessica-worker-test" {
		t.Fatalf("worker checkpoint=%q", cfg.Hosted.WorkerCheckpoint)
	}
}
