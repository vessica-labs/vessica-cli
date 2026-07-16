package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanClassifiesStackAndHarnessWithoutReadingSecrets(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=do-not-read\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := Scan(root, "git@github.com:Acme/Demo.git")
	if profile.Stack != "go" || profile.Harness != "absent" {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.Commands["test"] != "go test ./..." {
		t.Fatalf("commands=%v", profile.Commands)
	}
	if len(profile.EntryPoints) != 1 || profile.EntryPoints[0] != "cmd/api/main.go" || len(profile.Components) != 1 || profile.Components[0] != "cmd/api/" {
		t.Fatalf("entrypoints=%v components=%v", profile.EntryPoints, profile.Components)
	}
	if _, ok := profile.Fingerprint[".env"]; ok {
		t.Fatal("secret file entered repository profile")
	}
	if profile.Remote != "git@github.com:Acme/Demo.git" {
		t.Fatalf("remote=%q", profile.Remote)
	}
}

func TestScanPreservesPartialHarnessClassification(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".vessica"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".vessica", "harness.yaml"), []byte("test: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Scan(root, "https://github.com/acme/demo.git").Harness; got != "partial" {
		t.Fatalf("harness=%q", got)
	}
}
