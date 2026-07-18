package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRailwayObjectIDHandlesNestedResponses(t *testing.T) {
	for _, raw := range []string{
		`{"id":"sandbox-1"}`,
		`{"data":{"sandboxId":"sandbox-2"}}`,
		`[{"sandbox_id":"sandbox-3"}]`,
		"Warning: Railway sandboxes are experimental and may change.\n{\"id\":\"sandbox-4\"}",
	} {
		if id, err := railwayObjectID([]byte(raw)); err != nil || id == "" {
			t.Fatalf("raw=%s id=%q err=%v", raw, id, err)
		}
	}
}

func TestRailwayCLISessionEnvironmentOverridesServiceTokens(t *testing.T) {
	t.Setenv("HOME", "/stale-home")
	t.Setenv("RAILWAY_TOKEN", "stale-project")
	t.Setenv("RAILWAY_API_TOKEN", "stale-api")
	sandbox := NewRailway("/usr/bin/env", "project", "environment", "sandbox")
	sandbox.SetCLIHome("/private/railway-home")
	output, err := sandbox.command(context.Background()).Output()
	if err != nil {
		t.Fatal(err)
	}
	environment := string(output)
	if !strings.Contains(environment, "HOME=/private/railway-home") || strings.Contains(environment, "stale-project") || strings.Contains(environment, "stale-api") {
		t.Fatalf("unexpected environment: %s", environment)
	}
}

func TestRailwayExposePortRestartsCLIWithoutIdentityFlag(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "railway")
	script := "#!/bin/sh\nexec \"$VES_TEST_BINARY\" -test.run=TestRailwayForwardHelperProcess -- \"$@\"\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	countPath := filepath.Join(dir, "count")
	logPath := filepath.Join(dir, "args")
	t.Setenv("VES_TEST_BINARY", os.Args[0])
	t.Setenv("VES_FORWARD_COUNT", countPath)
	t.Setenv("VES_FORWARD_ARGS", logPath)
	t.Setenv("RAILWAY_API_TOKEN", "must-not-leak")
	var persisted atomic.Int32
	sandbox := NewRailway(cli, "project", "environment", "sandbox")
	sandbox.SetCLIHome(dir)
	sandbox.SetSessionPersist(func() error { persisted.Add(1); return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	forwardURL, err := sandbox.ExposePort(ctx, 4173)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.StopForward()
	response, err := http.Get(forwardURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "forwarded" {
		t.Fatalf("body=%q", body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(countPath)
		if strings.TrimSpace(string(raw)) == "2" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--identity-file") || strings.Contains(string(args), "must-not-leak") {
		t.Fatalf("unexpected forwarding invocation: %s", args)
	}
	if persisted.Load() == 0 {
		t.Fatal("expected CLI session to be persisted")
	}
}

func TestRailwayForwardHelperProcess(t *testing.T) {
	if os.Getenv("VES_FORWARD_COUNT") == "" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	countPath := os.Getenv("VES_FORWARD_COUNT")
	raw, _ := os.ReadFile(countPath)
	count, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	count++
	_ = os.WriteFile(countPath, []byte(strconv.Itoa(count)), 0o600)
	_ = os.WriteFile(os.Getenv("VES_FORWARD_ARGS"), []byte(fmt.Sprintf("%s home=%s token=%t", strings.Join(args, " "), os.Getenv("HOME"), os.Getenv("RAILWAY_API_TOKEN") != "")), 0o600)
	if count == 1 {
		os.Exit(1)
	}
	forward := args[len(args)-1]
	parts := strings.SplitN(forward, ":", 2)
	listener, err := net.Listen("tcp", "127.0.0.1:"+parts[0])
	if err != nil {
		os.Exit(2)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "forwarded") })}
	_ = server.Serve(listener)
	os.Exit(0)
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
