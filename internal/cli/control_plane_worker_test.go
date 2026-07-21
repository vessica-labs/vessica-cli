package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
)

func TestEnsureWorkerRepoUsesCheckpointDeltaAndKeepsDependencies(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	gitTest(t, base, "init", "--bare", remote)
	seed := filepath.Join(base, "seed")
	gitTest(t, base, "init", seed)
	gitTest(t, seed, "config", "user.email", "test@example.com")
	gitTest(t, seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "package.json"), []byte(`{"name":"demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "app.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "commit", "-m", "initial")
	gitTest(t, seed, "branch", "-M", "main")
	gitTest(t, seed, "remote", "add", "origin", remote)
	gitTest(t, seed, "push", "-u", "origin", "main")
	gitTest(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	root := filepath.Join(base, "workspace", "repo")
	gitTest(t, base, "clone", remote, root)
	fingerprint, err := reposnapshot.DependencyFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(reposnapshot.Checkpoint{BaseCommit: "initial", DependencyFingerprint: fingerprint, DependencyState: "ready"})
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), reposnapshot.MarkerFile), marker, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "app.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, seed, "add", "app.txt")
	gitTest(t, seed, "commit", "-m", "source change")
	gitTest(t, seed, "push", "origin", "main")

	result, err := ensureWorkerRepo(ctx, root, remote)
	if err != nil {
		t.Fatal(err)
	}
	if result["mode"] != "checkpoint_delta" || result["dependency_cache_hit"] != true {
		t.Fatalf("result=%#v", result)
	}
	content, _ := os.ReadFile(filepath.Join(root, "app.txt"))
	if string(content) != "two" {
		t.Fatalf("content=%q", content)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), reposnapshot.MarkerFile)); !os.IsNotExist(err) {
		t.Fatalf("marker was not consumed: %v", err)
	}
	candidateRaw, err := os.ReadFile(filepath.Join(filepath.Dir(root), reposnapshot.CandidateFile))
	if err != nil {
		t.Fatal(err)
	}
	var candidate reposnapshot.Checkpoint
	if json.Unmarshal(candidateRaw, &candidate) != nil || candidate.Status != "candidate" || candidate.VerifiedAt != "" {
		t.Fatalf("candidate=%#v", candidate)
	}
}

func TestConfigureHostedAuthAllowsAgentOnlyControlPlane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
	if err := configureHostedAuth(false); err != nil {
		t.Fatalf("optional hosted auth: %v", err)
	}
	if err := configureHostedAuth(true); err == nil {
		t.Fatal("coding worker auth should still require GITHUB_TOKEN")
	}
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := repo.GitCommandContext(context.Background(), args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
