package controlplane

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/sandbox"
)

// RunRailwayForwardSmoke proves that the deployed control plane can create a
// sandbox and reach its loopback HTTP server through Railway's native relay.
func RunRailwayForwardSmoke(ctx context.Context, cliPath, projectID, environmentID, checkpoint string, session *RailwayCLISession) error {
	if session == nil || !session.Ready() {
		return fmt.Errorf("Railway CLI session is not authorized")
	}
	rs := sandbox.NewRailway(cliPath, projectID, environmentID, "")
	rs.SetCLIHome(session.Home)
	rs.SetSessionPersist(func() error {
		persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return session.Persist(persistCtx)
	})
	env := map[string]string{}
	if strings.TrimSpace(checkpoint) != "" {
		env["VES_RAILWAY_CHECKPOINT"] = checkpoint
	}
	if err := rs.Create(ctx, sandbox.CreateOpts{Env: env, ExpiresAt: time.Now().Add(15 * time.Minute)}); err != nil {
		return fmt.Errorf("create forward smoke sandbox: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = rs.Destroy(cleanupCtx)
	}()

	script := `nohup node -e 'require("http").createServer((_,res)=>res.end("vessica-forward-smoke")).listen(4173,"127.0.0.1")' >/tmp/vessica-forward-smoke.log 2>&1 </dev/null & for n in $(seq 1 30); do curl -fsS http://127.0.0.1:4173 >/dev/null && exit 0; sleep 1; done; cat /tmp/vessica-forward-smoke.log >&2; exit 1`
	if code, err := rs.Exec(ctx, []string{"bash", "-lc", script}, io.Discard, io.Discard); err != nil || code != 0 {
		return fmt.Errorf("start forward smoke server: exit %d: %w", code, err)
	}
	forwardURL, err := rs.ExposePort(ctx, 4173)
	if err != nil {
		return fmt.Errorf("open forward smoke relay: %w", err)
	}
	defer rs.StopForward()

	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, forwardURL, nil)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("request forward smoke endpoint: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		return fmt.Errorf("read forward smoke endpoint: %w", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "vessica-forward-smoke" {
		return fmt.Errorf("forward smoke returned status %d with unexpected body", response.StatusCode)
	}
	return nil
}
