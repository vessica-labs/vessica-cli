package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRailwayObjectIDHandlesNestedResponses(t *testing.T) {
	for _, raw := range []string{
		`{"id":"sandbox-1"}`,
		`{"data":{"sandboxId":"sandbox-2"}}`,
		`[{"sandbox_id":"sandbox-3"}]`,
	} {
		if id, err := railwayObjectID([]byte(raw)); err != nil || id == "" {
			t.Fatalf("raw=%s id=%q err=%v", raw, id, err)
		}
	}
}

func TestRailwayCreateCapsIdleTimeoutAtPlatformMaximum(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "args.log")
	cli := filepath.Join(dir, "railway")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$VES_TEST_ARGS\"\nprintf 'Warning: experimental\\n' >&2\nprintf '{\"id\":\"sandbox-1\"}'\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VES_TEST_ARGS", logPath)
	sandbox := NewRailway(cli, "project", "environment", "")
	if err := sandbox.Create(context.Background(), CreateOpts{ExpiresAt: time.Now().Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--idle-timeout-minutes 120") {
		t.Fatalf("args=%s", args)
	}
}

func TestRailwayCommandUsesRefreshedAPITokenOnly(t *testing.T) {
	t.Setenv("RAILWAY_TOKEN", "stale-project")
	t.Setenv("RAILWAY_API_TOKEN", "stale-api")
	sandbox := NewRailway("/usr/bin/env", "project", "environment", "sandbox")
	sandbox.SetAPIToken("fresh-oauth")
	output, err := sandbox.command(context.Background()).Output()
	if err != nil {
		t.Fatal(err)
	}
	environment := string(output)
	if !strings.Contains(environment, "RAILWAY_API_TOKEN=fresh-oauth") || strings.Contains(environment, "stale-project") || strings.Contains(environment, "stale-api") {
		t.Fatalf("unexpected environment: %s", environment)
	}
}
