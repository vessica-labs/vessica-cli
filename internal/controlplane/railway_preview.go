package controlplane

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (l *RailwayLauncher) SandboxLogs(ctx context.Context, record *state.Sandbox) (string, error) {
	if record == nil || record.ContainerID == "" {
		return "", fmt.Errorf("Railway sandbox is unavailable")
	}
	rs := sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, record.ContainerID)
	if err := l.configureAuth(ctx, rs); err != nil {
		return "", err
	}
	var output bytes.Buffer
	code, err := rs.Exec(ctx, []string{"bash", "-lc", "tail -n 200 /workspace/repo/.vessica-preview.log /workspace/.vessica-preview.log /workspace/.vessica/preview-tunnel.log 2>/dev/null || true"}, &output, &output)
	if err != nil || code != 0 {
		return "", fmt.Errorf("read Railway sandbox logs: exit %d: %w", code, err)
	}
	return output.String(), nil
}

func (l *RailwayLauncher) publishPreview(ctx context.Context, rs *sandbox.RailwaySandbox, runRecord *state.Run, sandboxRecord *state.Sandbox) (string, error) {
	previewBase := strings.TrimRight(firstNonEmptyCP(l.PreviewPublicURL, l.PublicURL), "/")
	parsed, err := url.Parse(previewBase)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "localhost" || net.ParseIP(parsed.Hostname()).IsLoopback() {
		return "", fmt.Errorf("public preview origin must be a non-loopback HTTPS URL")
	}
	if err := l.Broker.RegisterTunnel(runRecord.ID); err != nil {
		return "", err
	}
	if err := l.startPreviewTunnel(ctx, rs, runRecord.ID, sandboxRecord.PreviewPort); err != nil {
		return "", err
	}
	capability, err := l.Broker.Issue(runRecord.ID, 7*24*time.Hour)
	if err != nil {
		return "", err
	}
	publicURL := previewBase + "/previews/" + url.PathEscape(runRecord.ID) + "/?cap=" + url.QueryEscape(capability)
	if err := waitForPublicPreview(ctx, publicURL, 60*time.Second); err != nil {
		return "", err
	}
	return publicURL, nil
}

func (l *RailwayLauncher) startPreviewTunnel(ctx context.Context, rs *sandbox.RailwaySandbox, runID string, port int) error {
	workerURL := strings.TrimRight(l.PublicURL, "/") + "/internal/worker/ves"
	localURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	script := strings.Join([]string{
		"set -euo pipefail",
		"install -d -o vessica-agent -g vessica-agent -m 0700 /workspace/.vessica",
		"curl -fsSL -H \"Authorization: Bearer $VES_WORKER_DOWNLOAD_TOKEN\" " + shellQuoteCP(workerURL) + " -o /workspace/.vessica/ves-preview-tunnel",
		"chown vessica-agent:vessica-agent /workspace/.vessica/ves-preview-tunnel",
		"chmod 0700 /workspace/.vessica/ves-preview-tunnel",
		"test -f /workspace/.vessica/preview-tunnel.pid && kill $(cat /workspace/.vessica/preview-tunnel.pid) 2>/dev/null || true",
		"runuser -u vessica-agent --preserve-environment -- bash -lc " + shellQuoteCP("HOME=/home/vessica-agent nohup /workspace/.vessica/ves-preview-tunnel control-plane preview-tunnel --run-id "+shellQuoteCP(runID)+" --local-url "+shellQuoteCP(localURL)+" --control-url "+shellQuoteCP(l.PublicURL)+" >/workspace/.vessica/preview-tunnel.log 2>&1 </dev/null & echo $! >/workspace/.vessica/preview-tunnel.pid"),
	}, "\n")
	var output bytes.Buffer
	code, err := rs.Exec(ctx, []string{"bash", "-lc", script}, &output, &output)
	if err != nil || code != 0 {
		return fmt.Errorf("start authenticated preview tunnel: exit %d: %w: %s", code, err, strings.TrimSpace(output.String()))
	}
	return nil
}

func waitForPublicPreview(ctx context.Context, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if response, err := client.Do(req); err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("public preview did not become healthy within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func firstNonEmptyCP(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
