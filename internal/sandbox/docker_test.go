package sandbox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitForHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if err := waitForHTTP(context.Background(), srv.URL, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestDockerNodePreviewReloads(t *testing.T) {
	if os.Getenv("VES_TEST_DOCKER_PREVIEW") != "1" {
		t.Skip("set VES_TEST_DOCKER_PREVIEW=1 to run Docker preview integration test")
	}
	if exec.Command("docker", "image", "inspect", "node:24-bookworm").Run() != nil {
		t.Skip("node:24-bookworm image is not available")
	}
	root := t.TempDir()
	server := `import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';
const message = readFileSync('message.txt', 'utf8').trim();
createServer((_req, res) => res.end(message)).listen(process.env.PORT || 3000, '0.0.0.0');
`
	if err := os.WriteFile(filepath.Join(root, "server.mjs"), []byte(server), 0o644); err != nil {
		t.Fatal(err)
	}
	messagePath := filepath.Join(root, "message.txt")
	if err := os.WriteFile(messagePath, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := fmt.Sprintf("sbx_preview_%d", time.Now().UnixNano())
	d := NewDocker(id)
	if err := d.Create(ctx, CreateOpts{
		SandboxID:   id,
		WorkspaceID: "ws_test",
		RunID:       "run_preview_test",
		Image:       "node:24-bookworm",
		HostWorkdir: root,
		PreviewPort: 3000,
		ExpiresAt:   time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	defer d.Destroy(context.Background())
	if err := d.Start(ctx); err != nil {
		t.Fatal(err)
	}
	url, err := d.StartPreview(ctx, "PORT=3000 node --watch-path=. server.mjs", 3000, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := fetchBody(t, url); got != "before" {
		t.Fatalf("initial body=%q", got)
	}
	if err := os.WriteFile(messagePath, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got := fetchBodyNoFail(url); got == "after" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("Node preview did not reload after source change")
}

func fetchBody(t *testing.T, url string) string {
	t.Helper()
	body := fetchBodyNoFail(url)
	if body == "" {
		t.Fatalf("empty response from %s", url)
	}
	return body
}

func fetchBodyNoFail(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func TestWaitForHTTPFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := waitForHTTP(ctx, "http://127.0.0.1:1", time.Second); err == nil {
		t.Fatal("expected failure")
	}
}

func TestPreviewStartupTimeoutAllowsHostedColdStarts(t *testing.T) {
	t.Setenv("VES_PREVIEW_STARTUP_TIMEOUT", "")
	t.Setenv("VES_CODEX_EXTERNAL_SANDBOX", "1")
	if got := previewStartupTimeout(); got != 2*time.Minute {
		t.Fatalf("previewStartupTimeout() = %s, want 2m", got)
	}

	t.Setenv("VES_PREVIEW_STARTUP_TIMEOUT", "45s")
	if got := previewStartupTimeout(); got != 45*time.Second {
		t.Fatalf("previewStartupTimeout() = %s, want 45s", got)
	}
}

func TestRewriteHealthcheckURL(t *testing.T) {
	got := rewriteHealthcheckURL("http://localhost:3000/health", "http://127.0.0.1:49152")
	if got != "http://127.0.0.1:49152/health" {
		t.Fatalf("got %q", got)
	}
}
