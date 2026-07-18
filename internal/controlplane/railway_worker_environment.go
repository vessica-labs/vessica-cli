package controlplane

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func (l *RailwayLauncher) workerEnvironment(runID, repositoryRemote, checkpoint string, requestedAt time.Time) map[string]string {
	service := "control-plane"
	databaseVariable := "VES_CONTROL_DATABASE_URL"
	// Railway sandbox private networking becomes available well after a warm
	// checkpoint fork in some environments. Operators can provide the database
	// public proxy URL for short-lived workers while the long-running control
	// plane continues to use the lower-latency private URL.
	if strings.TrimSpace(os.Getenv("VES_CONTROL_DATABASE_WORKER_URL")) != "" {
		databaseVariable = "VES_CONTROL_DATABASE_WORKER_URL"
	}
	return map[string]string{
		"VES_RAILWAY_CHECKPOINT":      checkpoint,
		"VES_RUN_ID":                  runID,
		"VES_SANDBOX_REQUESTED_AT_MS": fmt.Sprint(requestedAt.UnixMilli()),
		"VES_CONTROL_DATABASE_URL":    service + "." + databaseVariable,
		"VES_STATE_BACKEND":           "postgres-url",
		"VES_CONTROL_PLANE_URL":       l.PublicURL,
		"VES_WORKER_DOWNLOAD_TOKEN":   service + ".VES_WORKER_DOWNLOAD_TOKEN",
		"VES_REPO_REMOTE":             repositoryRemote,
		"VES_RUNNER_MODEL":            service + ".VES_RUNNER_MODEL",
		"VES_RUNNER_REASONING_EFFORT": service + ".VES_RUNNER_REASONING_EFFORT",
		"VES_KNOWLEDGE_MODE":          service + ".VES_KNOWLEDGE_MODE",
		"VES_KNOWLEDGE_ENDPOINT":      service + ".VES_KNOWLEDGE_ENDPOINT",
		"VES_KNOWLEDGE_TOKEN":         service + ".VES_KNOWLEDGE_TOKEN",
		"VES_KNOWLEDGE_WORKSPACE_ID":  service + ".VES_KNOWLEDGE_WORKSPACE_ID",
		"VES_CODEX_EXTERNAL_SANDBOX":  "1",
		"VES_CODEX_MCP_SERVERS_FILE":  "/opt/vessica/codex-mcp-servers.json",
		"VES_RUNNER_USER":             "vessica-agent",
		"VES_RUNNER_HOME":             "/home/vessica-agent",
		"GITHUB_TOKEN":                service + ".GITHUB_TOKEN",
		"OPENAI_API_KEY":              service + ".OPENAI_API_KEY",
		"VES_CODEX_AUTH_B64":          service + ".VES_CODEX_AUTH_B64",
	}
}
