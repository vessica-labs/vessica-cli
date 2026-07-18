package controlplane

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

var (
	previewHTMLTagPattern  = regexp.MustCompile(`(?is)<(?:link|script)\b[^>]*>`)
	previewHTMLAttrPattern = regexp.MustCompile(`(?is)([a-z][a-z0-9_-]*)\s*=\s*["']([^"']*)["']`)
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
	logCommand := strings.Join([]string{
		"for log in /workspace/repo/.vessica-preview.log /workspace/.vessica-preview.log /workspace/repo/.vessica/sandboxes/*/workspace/.vessica-preview.log; do",
		"  test -f \"$log\" || continue",
		"  printf '==> %s <==\\n' \"$log\"",
		"  tail -n 200 \"$log\"",
		"done",
	}, "\n")
	code, err := rs.Exec(ctx, []string{"bash", "-lc", logCommand}, &output, &output)
	if err != nil || code != 0 {
		return "", fmt.Errorf("read Railway sandbox logs: exit %d: %w", code, err)
	}
	return output.String(), nil
}

func (l *RailwayLauncher) publishPreview(ctx context.Context, rs *sandbox.RailwaySandbox, runRecord *state.Run, sandboxRecord *state.Sandbox) (string, error) {
	if !rs.UsesCLISession() {
		return "", fmt.Errorf("Railway CLI forwarding session is not authorized")
	}
	forwardURL, err := rs.ExposePort(ctx, sandboxRecord.PreviewPort)
	if err != nil {
		return "", err
	}
	if err := l.Broker.Register(runRecord.ID, forwardURL, func() { _ = rs.StopForward() }); err != nil {
		_ = rs.StopForward()
		return "", err
	}
	publicURL, err := l.issuePublicPreviewURL(runRecord.ID)
	if err != nil {
		return "", err
	}
	if err := waitForPublicPreview(ctx, publicURL, 60*time.Second); err != nil {
		l.Broker.Remove(runRecord.ID)
		return "", err
	}
	return publicURL, nil
}

func (l *RailwayLauncher) issuePublicPreviewURL(runID string) (string, error) {
	capability, err := l.Broker.Issue(runID, 7*24*time.Hour)
	if err != nil {
		return "", err
	}
	return l.publicPreviewURL(runID, capability)
}

func (l *RailwayLauncher) publicPreviewURL(runID, capability string) (string, error) {
	previewBase := strings.TrimRight(strings.TrimSpace(l.PreviewPublicURL), "/")
	if previewBase == "" {
		return "", fmt.Errorf("VES_PREVIEW_ORIGIN is required for hosted previews")
	}
	parsed, err := url.Parse(previewBase)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "localhost" || net.ParseIP(parsed.Hostname()).IsLoopback() {
		return "", fmt.Errorf("public preview origin must be a non-loopback HTTPS URL")
	}
	if strings.TrimSpace(capability) == "" {
		return "", fmt.Errorf("preview capability is required")
	}
	return previewBase + "/previews/" + url.PathEscape(runID) + "/?cap=" + url.QueryEscape(capability), nil
}

func waitForPublicPreview(ctx context.Context, target string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar}
	lastError := "preview endpoint is unavailable"
	for {
		if err := validatePublicPreviewAttempt(ctx, client, target); err == nil {
			return nil
		} else {
			lastError = err.Error()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("public preview did not become healthy within %s: %s", timeout, lastError)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func validatePublicPreviewAttempt(ctx context.Context, client *http.Client, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	_ = response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return fmt.Errorf("preview returned HTTP %d", response.StatusCode)
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return nil
	}
	base, err := url.Parse(target)
	if err != nil {
		return err
	}
	for _, asset := range previewBrowserAssets(base, body) {
		assetReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL.String(), nil)
		assetReq.Header.Set("Referer", target)
		if asset.Kind == "style" {
			assetReq.Header.Set("Accept", "text/css,*/*;q=0.1")
			assetReq.Header.Set("Sec-Fetch-Dest", "style")
		} else {
			assetReq.Header.Set("Accept", "*/*")
			assetReq.Header.Set("Sec-Fetch-Dest", "script")
		}
		assetResponse, assetErr := client.Do(assetReq)
		if assetErr != nil {
			return fmt.Errorf("preview asset %s is unavailable: %w", asset.URL.Path, assetErr)
		}
		_ = assetResponse.Body.Close()
		if assetResponse.StatusCode < 200 || assetResponse.StatusCode >= 400 {
			return fmt.Errorf("preview asset %s returned HTTP %d", asset.URL.Path, assetResponse.StatusCode)
		}
		contentType := strings.ToLower(assetResponse.Header.Get("Content-Type"))
		if asset.Kind == "style" && !strings.Contains(contentType, "text/css") {
			return fmt.Errorf("preview stylesheet %s returned %s", asset.URL.Path, firstNonEmptyCP(contentType, "no content type"))
		}
		if asset.Kind == "script" && !strings.Contains(contentType, "javascript") && !strings.Contains(contentType, "ecmascript") && !strings.Contains(contentType, "wasm") {
			return fmt.Errorf("preview script %s returned %s", asset.URL.Path, firstNonEmptyCP(contentType, "no content type"))
		}
	}
	return nil
}

type previewBrowserAsset struct {
	URL  *url.URL
	Kind string
}

func previewBrowserAssets(base *url.URL, body []byte) []previewBrowserAsset {
	assets := make([]previewBrowserAsset, 0, 8)
	seen := map[string]bool{}
	for _, rawTag := range previewHTMLTagPattern.FindAll(body, -1) {
		tag := strings.ToLower(string(rawTag))
		attributes := map[string]string{}
		for _, match := range previewHTMLAttrPattern.FindAllSubmatch(rawTag, -1) {
			attributes[strings.ToLower(string(match[1]))] = html.UnescapeString(string(match[2]))
		}
		kind, reference := "", attributes["href"]
		if strings.HasPrefix(tag, "<script") {
			kind, reference = "script", attributes["src"]
		} else if strings.Contains(strings.ToLower(attributes["rel"]), "stylesheet") {
			kind = "style"
		} else if strings.Contains(strings.ToLower(attributes["rel"]), "modulepreload") {
			kind = "script"
		}
		if kind == "" || strings.TrimSpace(reference) == "" {
			continue
		}
		assetURL, err := base.Parse(reference)
		if err != nil || !strings.EqualFold(assetURL.Host, base.Host) || seen[assetURL.String()] {
			continue
		}
		seen[assetURL.String()] = true
		assets = append(assets, previewBrowserAsset{URL: assetURL, Kind: kind})
		if len(assets) == 8 {
			break
		}
	}
	return assets
}

func firstNonEmptyCP(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
