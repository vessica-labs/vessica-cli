package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectOmitsGuessedPreviewAndFindsSingleGoCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Detect(root).PreviewCommand; got != "" {
		t.Fatalf("guessed preview command=%q", got)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "api", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Detect(root).PreviewCommand; got != "go run ./cmd/api" {
		t.Fatalf("preview command=%q", got)
	}
}

func TestWriteStartingFilesUsesRepositoryFindings(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	findings := RepositoryFindings{Name: "demo", Remote: "https://github.com/acme/demo.git", Stack: "go", Components: []string{"cmd/api/"}, EntryPoints: []string{"cmd/api/main.go"}, Directories: []string{"cmd/", "internal/"}, Commands: map[string]string{"build": "go build ./...", "test": "go test ./..."}}
	if err := WriteStartingFiles(root, []string{"ARCHITECTURE.md"}, findings); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, expected := range []string{"cmd/api/", "cmd/api/main.go", "https://github.com/acme/demo.git", "go build ./..."} {
		if !strings.Contains(text, expected) {
			t.Fatalf("generated architecture missing %q:\n%s", expected, text)
		}
	}
}
