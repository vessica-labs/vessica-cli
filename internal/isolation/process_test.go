package isolation

import (
	"strings"
	"testing"
)

func TestRepositoryEnvironmentExcludesModelAndControlPlaneSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")
	t.Setenv("OPENAI_API_KEY", "model-secret")
	t.Setenv("VES_RUNNER_HOME", "/tmp/isolated-home")
	env := strings.Join(Environment(nil), "\n")
	for _, forbidden := range []string{"DATABASE_URL=", "GITHUB_TOKEN=", "OPENAI_API_KEY="} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("repository command inherited %s: %s", forbidden, env)
		}
	}
	if !strings.Contains(env, "HOME=/tmp/isolated-home") {
		t.Fatalf("repository command missing isolated home: %s", env)
	}
}

func TestModelEnvironmentIncludesOnlyExplicitModelCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "model-secret")
	t.Setenv("DATABASE_URL", "postgres://secret")
	env := strings.Join(ModelEnvironment(nil), "\n")
	if !strings.Contains(env, "OPENAI_API_KEY=model-secret") || strings.Contains(env, "DATABASE_URL=") {
		t.Fatalf("model environment=%s", env)
	}
}

func TestExactSafeDirectoryRegexQuotesPathAndRejectsWildcard(t *testing.T) {
	got := exactSafeDirectoryRegex(`/tmp/a.b/[ticket]`)
	want := `^/tmp/a\.b/\[ticket\]$`
	if got != want {
		t.Fatalf("regex=%q want=%q", got, want)
	}
	if got == "*" {
		t.Fatal("safe.directory must never use a wildcard")
	}
}
