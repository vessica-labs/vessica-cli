package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func TestConnectLinearIntegrationReconnectPropagatesOAuthWhenConfigUnchanged(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("VES_AUTH_STORE", "file")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	commandLog := filepath.Join(root, "railway.log")
	deployedMarker := filepath.Join(root, "redeployed")
	railway := filepath.Join(bin, "railway")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$VES_TEST_COMMAND_LOG"
if [ "$1 $2" = "deployment list" ]; then
  if [ -f "$VES_TEST_DEPLOYED_MARKER" ]; then
    printf '%s' '[{"id":"deploy-new","status":"SUCCESS"}]'
  else
    printf '%s' '[{"id":"deploy-old","status":"SUCCESS"}]'
  fi
elif [ "$1" = "redeploy" ]; then
  : > "$VES_TEST_DEPLOYED_MARKER"
  printf '{}'
else
  printf '{}'
fi
`
	if err := os.WriteFile(railway, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RAILWAY_TOKEN", "test-railway-token")
	t.Setenv("VES_TEST_COMMAND_LOG", commandLog)
	t.Setenv("VES_TEST_DEPLOYED_MARKER", deployedMarker)

	trackerConfig := config.TrackerConfig{
		Provider: "linear", Mode: "best_efforts", TeamID: "team-1", ProjectID: "project-1",
		TodoStateID: "todo-1", WIPStateID: "wip-1", DoneStateID: "done-1", TriggerLabel: "Vessica",
	}
	controlPlane := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			raw, _ := json.Marshal(trackerConfig)
			_ = json.NewEncoder(w).Encode(map[string]any{"integration": map[string]any{"status": "connected", "config_json": string(raw)}})
		case "/readyz":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer controlPlane.Close()

	linearAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if strings.Contains(request.Query, "VessicaTrackerDiscovery") {
			_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[{"id":"team-1","name":"Team","key":"TEAM","states":{"nodes":[{"id":"todo-1","name":"Todo","type":"unstarted"},{"id":"wip-1","name":"In Progress","type":"started"},{"id":"done-1","name":"Done","type":"completed"}]}}]},"projects":{"nodes":[{"id":"project-1","name":"Project","slugId":"project","teams":{"nodes":[{"id":"team-1","name":"Team","key":"TEAM"}]}}]}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"issueLabels":{"nodes":[{"id":"label-1","name":"Vessica"}]}}}`))
	}))
	defer linearAPI.Close()

	cfg := config.Defaults()
	cfg.Hosted = config.HostedConfig{
		WorkspaceID: "railway-workspace", ProjectID: "railway-project", EnvironmentID: "production", ServiceID: "control-plane",
		ControlPlaneURL: controlPlane.URL,
	}
	cfg.Tracker = trackerConfig
	app := &App{Root: root, Config: cfg}
	linear := tracker.NewLinearClient("new-workspace-access")
	linear.Endpoint = linearAPI.URL
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := connectLinearIntegrationWithClient(ctx, app, linearIntegrationOptions{}, railwaySecrets{
		APIToken: "control-plane-token", WebhookSecret: "webhook-secret", WebhookID: "webhook-1",
	}, `{"provider":"linear","access_token":"new-workspace-access","updated_at":"2026-07-17T01:00:00Z"}`, "new-workspace-access", linear)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged, _ := result["unchanged"].(bool); !unchanged {
		t.Fatalf("result=%#v", result)
	}
	commands, err := os.ReadFile(commandLog)
	if err != nil {
		t.Fatal(err)
	}
	log := string(commands)
	if !strings.Contains(log, "variable set VES_LINEAR_OAUTH_JSON --stdin --skip-deploys") {
		t.Fatalf("OAuth credential was not propagated:\n%s", log)
	}
	if !strings.Contains(log, "redeploy --project railway-project -e production -s control-plane --yes") {
		t.Fatalf("control plane was not redeployed:\n%s", log)
	}
}
