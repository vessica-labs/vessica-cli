package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestHostedSaveWritesOnlyRepositoryAttachmentDescriptor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	cfg := Defaults()
	cfg.Kind = "RepositoryAttachment"
	cfg.Attachment = AttachmentConfig{WorkspaceID: "ws_hosted", RepositoryID: "repo_hosted"}
	cfg.Hosted = HostedConfig{Provider: "railway", ProjectID: "secret-project-detail", ControlPlaneURL: "https://control.example"}
	cfg.Repo.Remote = "https://github.com/acme/service.git"
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{"kind: RepositoryAttachment", "id: ws_hosted", "id: repo_hosted", "endpoint: https://control.example", "remote: https://github.com/acme/service.git"} {
		if !strings.Contains(text, required) {
			t.Fatalf("descriptor missing %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{"state:", "sandbox:", "project_id:", "secret-project-detail"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("descriptor contains %q:\n%s", forbidden, text)
		}
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Attachment != cfg.Attachment || loaded.Repo.Remote != cfg.Repo.Remote || loaded.Hosted.ControlPlaneURL != cfg.Hosted.ControlPlaneURL {
		t.Fatalf("loaded=%#v", loaded)
	}
	if loaded.State.Backend != "hosted" || loaded.Knowledge.Mode != "hosted" {
		t.Fatalf("attachment fell back to local authority: %#v", loaded)
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

func TestHostedAuthorityRejectsLocalEnvironmentFallback(t *testing.T) {
	cfg := HostedDefaults()
	cfg.Attachment = AttachmentConfig{WorkspaceID: "ws_hosted", RepositoryID: "repo_hosted"}
	cfg.Hosted.ControlPlaneURL = "https://control.example"
	t.Setenv("VES_STATE_BACKEND", "sqlite")
	t.Setenv("VES_KNOWLEDGE_MODE", "local")
	ApplyEnv(&cfg)
	EnforceHostedAuthority(&cfg)
	if cfg.State.Backend != "hosted" || cfg.Knowledge.Mode != "hosted" || cfg.Knowledge.LocalPath != "" {
		t.Fatalf("hosted authority = %#v", cfg)
	}
}
