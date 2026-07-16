package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

type railwayInstallationCandidate struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	EnvironmentID string `json:"environment_id"`
	ServiceID     string `json:"service_id"`
	Endpoint      string `json:"endpoint"`
}

type railwayProjectListItem struct {
	Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Environments struct {
		Edges []struct {
			Node struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"environments"`
	Services struct {
		Edges []struct {
			Node struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"services"`
}

func verifyAttachedInstallation(ctx context.Context, app *App) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(app.Root)
	if err != nil {
		return nil, err
	}
	var status map[string]any
	endpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/status"
	if err := hostedRequest(ctx, "GET", endpoint, secrets.APIToken, nil, &status); err != nil {
		return nil, err
	}
	if _, err := runRailway(ctx, "", nil, "sandbox", "list", "-p", app.Config.Hosted.ProjectID, "-e", app.Config.Hosted.EnvironmentID, "--json"); err != nil {
		return nil, err
	}
	return map[string]any{"status": "running", "reused": true, "control_plane": status}, nil
}

func sandboxFeatureError(err error) bool {
	v := strings.ToLower(err.Error())
	return strings.Contains(v, "project_sandboxes") || strings.Contains(v, "priority boarding") || strings.Contains(v, "sandbox") && strings.Contains(v, "not enabled")
}

func railwayUpPreflight(ctx context.Context, root string) map[string]any {
	result := map[string]any{"authenticated": false, "sandbox_entitlement": "unknown"}
	if raw, err := runRailwaySession(ctx, "", nil, "whoami", "--json"); err == nil {
		result["authenticated"] = true
		var account struct {
			Workspaces []railwayWorkspaceChoice `json:"workspaces"`
		}
		if json.Unmarshal(raw, &account) == nil {
			result["workspaces"] = account.Workspaces
		}
		if candidates, discoverErr := discoverRailwayInstallations(ctx, ""); discoverErr == nil {
			result["vessica_installations"] = candidates
		} else {
			result["installation_discovery_error"] = discoverErr.Error()
		}
	} else {
		result["authentication_error"] = err.Error()
	}
	if cfg, err := config.Load(root); err == nil && cfg.Hosted.ProjectID != "" {
		_, probeErr := runRailway(ctx, "", nil, "sandbox", "list", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "--json")
		switch {
		case probeErr == nil:
			result["sandbox_entitlement"] = "enabled"
		case sandboxFeatureError(probeErr):
			result["sandbox_entitlement"] = "disabled"
			result["action_url"] = "https://railway.com/account/feature-flags"
		default:
			result["sandbox_error"] = probeErr.Error()
		}
	}
	return result
}

func discoverRailwayInstallations(ctx context.Context, workspace string) ([]railwayInstallationCandidate, error) {
	raw, err := runRailwaySession(ctx, "", nil, "list", "--json")
	if err != nil {
		return nil, err
	}
	var projects []railwayProjectListItem
	if err := json.Unmarshal(raw, &projects); err != nil {
		return nil, fmt.Errorf("decode Railway project list: %w", err)
	}
	candidates := make([]railwayInstallationCandidate, 0)
	for _, project := range projects {
		if workspace != "" && !strings.EqualFold(workspace, project.Workspace.ID) && !strings.EqualFold(workspace, project.Workspace.Name) {
			continue
		}
		candidate, ok := vessicaProjectCandidate(project)
		if !ok {
			continue
		}
		domainsRaw, domainErr := runRailway(ctx, "", nil, "domain", "list", "--project", candidate.ProjectID, "--environment", candidate.EnvironmentID, "--service", candidate.ServiceID, "--json")
		if domainErr != nil {
			continue
		}
		var domains struct {
			Domains []struct {
				Domain     string `json:"domain"`
				SyncStatus string `json:"syncStatus"`
			} `json:"domains"`
		}
		if json.Unmarshal(domainsRaw, &domains) != nil {
			continue
		}
		for _, domain := range domains.Domains {
			if domain.Domain != "" && (domain.SyncStatus == "" || strings.EqualFold(domain.SyncStatus, "active")) {
				candidate.Endpoint = "https://" + strings.TrimPrefix(domain.Domain, "https://")
				break
			}
		}
		if candidate.Endpoint == "" || !verifiedInstallationIdentity(ctx, candidate) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func vessicaProjectCandidate(project railwayProjectListItem) (railwayInstallationCandidate, bool) {
	candidate := railwayInstallationCandidate{WorkspaceID: project.Workspace.ID, WorkspaceName: project.Workspace.Name, ProjectID: project.ID, ProjectName: project.Name}
	controlPlane, knowledge := false, false
	for _, edge := range project.Environments.Edges {
		if strings.EqualFold(edge.Node.Name, "production") {
			candidate.EnvironmentID = edge.Node.ID
		}
	}
	for _, edge := range project.Services.Edges {
		switch strings.ToLower(edge.Node.Name) {
		case "control-plane":
			controlPlane, candidate.ServiceID = true, edge.Node.ID
		case "knowledge-server":
			knowledge = true
		}
	}
	return candidate, controlPlane && knowledge && candidate.EnvironmentID != ""
}

func verifiedInstallationIdentity(ctx context.Context, candidate railwayInstallationCandidate) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(candidate.Endpoint, "/")+"/readyz", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var identity struct {
		InstallationID string `json:"installation_id"`
	}
	return json.NewDecoder(response.Body).Decode(&identity) == nil && identity.InstallationID == candidate.ProjectID
}

func recoverRailwayInstallation(ctx context.Context, candidate railwayInstallationCandidate) (config.Config, railwaySecrets, error) {
	cfg := config.Defaults()
	cfg.Sandbox.Backend = "railway"
	cfg.Hosted = config.HostedConfig{Provider: "railway", WorkspaceID: candidate.WorkspaceID, ProjectID: candidate.ProjectID, EnvironmentID: candidate.EnvironmentID, ServiceID: candidate.ServiceID, ControlPlaneURL: candidate.Endpoint}
	if err := reconcileRailwayResourceIDs(ctx, &cfg); err != nil {
		return cfg, railwaySecrets{}, err
	}
	controlVariables, err := readRailwayVariables(ctx, cfg, cfg.Hosted.ServiceID)
	if err != nil {
		return cfg, railwaySecrets{}, err
	}
	cfg.Hosted.WorkerCheckpoint = controlVariables["VES_RAILWAY_CHECKPOINT"]
	cfg.Knowledge.Mode = "hosted"
	cfg.Knowledge.Endpoint = controlVariables["VES_KNOWLEDGE_ENDPOINT"]
	cfg.Knowledge.WorkspaceID = controlVariables["VES_KNOWLEDGE_WORKSPACE_ID"]
	knowledgeVariables, err := readRailwayVariables(ctx, cfg, cfg.Knowledge.ServiceID)
	if err != nil {
		return cfg, railwaySecrets{}, err
	}
	secrets := railwaySecrets{
		RuntimeToken:              controlVariables["RAILWAY_TOKEN"],
		ServiceToken:              controlVariables["VES_CONTROL_PLANE_API_TOKEN"],
		WorkerToken:               controlVariables["VES_WORKER_DOWNLOAD_TOKEN"],
		WebhookSecret:             controlVariables["VES_LINEAR_WEBHOOK_SECRET"],
		WebhookID:                 controlVariables["VES_LINEAR_WEBHOOK_ID"],
		CredentialKey:             controlVariables["VES_CREDENTIAL_ENCRYPTION_KEY"],
		KnowledgeToken:            knowledgeVariables["KNOWLEDGE_API_TOKEN"],
		KnowledgeAdminToken:       knowledgeVariables["KNOWLEDGE_EXPORT_TOKEN"],
		ControlDatabasePassword:   databasePasswordFromURL(controlVariables["VES_CONTROL_DATABASE_URL"], controlDatabaseRole),
		KnowledgeDatabasePassword: databasePasswordFromURL(knowledgeVariables["VES_KNOWLEDGE_DATABASE_URL"], knowledgeDatabaseRole),
	}
	if secrets.ServiceToken == "" || secrets.KnowledgeToken == "" || secrets.KnowledgeAdminToken == "" || cfg.Knowledge.Endpoint == "" {
		return cfg, railwaySecrets{}, fmt.Errorf("verified installation is missing required Vessica identity or credential variables")
	}
	githubToken, err := auth.Token("github")
	if err != nil {
		return cfg, railwaySecrets{}, err
	}
	subject, err := auth.ValidateGitHubToken(githubToken)
	if err != nil {
		return cfg, railwaySecrets{}, err
	}
	var credential struct {
		Token string `json:"token"`
	}
	if err := hostedRequest(ctx, http.MethodPost, strings.TrimRight(cfg.Hosted.ControlPlaneURL, "/")+"/api/v1/cli-credentials", secrets.ServiceToken, map[string]string{"subject": subject}, &credential); err != nil {
		return cfg, railwaySecrets{}, err
	}
	if credential.Token == "" {
		return cfg, railwaySecrets{}, fmt.Errorf("control plane returned an empty recovered CLI credential")
	}
	secrets.APIToken = credential.Token
	return cfg, secrets, nil
}

func readRailwayVariables(ctx context.Context, cfg config.Config, serviceID string) (map[string]string, error) {
	if serviceID == "" {
		return nil, fmt.Errorf("Railway service id is required for installation recovery")
	}
	raw, err := runRailway(ctx, "", nil, "variable", "list", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", serviceID, "--json")
	if err != nil {
		return nil, err
	}
	variables := map[string]string{}
	if err := json.Unmarshal(raw, &variables); err != nil {
		return nil, fmt.Errorf("decode Railway service variables: %w", err)
	}
	return variables, nil
}

var commitPattern = regexp.MustCompile(`(?m)^[0-9a-f]{40}$`)

type railwayOrientation struct {
	Commit string
	Files  []string
}

func runRailwayOrientation(ctx context.Context, cfg config.Config, remote string) (railwayOrientation, error) {
	cloneRemote := orientationCloneRemote(remote)
	base := []string{"sandbox", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID}
	args := append(append([]string{}, base...), "create", "--checkpoint", cfg.Hosted.WorkerCheckpoint, "--private-network", "--idle-timeout-minutes", "20", "--variable", "VES_REPO_REMOTE="+cloneRemote, "--variable", "GITHUB_TOKEN=control-plane.GITHUB_TOKEN", "--json")
	raw, err := runRailway(ctx, "", nil, args...)
	if err != nil {
		return railwayOrientation{}, err
	}
	sandboxID, err := objectID(raw)
	if err != nil {
		return railwayOrientation{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		_, _ = runRailway(cleanup, "", nil, append(base, "destroy", "--id", sandboxID)...)
	}()
	script := `set -euo pipefail
askpass=$(mktemp)
trap 'rm -f "$askpass"' EXIT
cat >"$askpass" <<'EOF'
#!/usr/bin/env bash
case "$1" in *Username*) printf '%s\n' x-access-token ;; *) printf '%s\n' "$GITHUB_TOKEN" ;; esac
EOF
chmod 0700 "$askpass"
rm -rf /tmp/vessica-orientation
GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 git clone --quiet --depth=1 "$VES_REPO_REMOTE" /tmp/vessica-orientation
git -C /tmp/vessica-orientation rev-parse HEAD
find /tmp/vessica-orientation -maxdepth 3 -type f -not -path '*/.git/*' | sed 's#^/tmp/vessica-orientation/##' | sort | head -500 | sed 's#^#VES_FILE #'
`
	out, err := runRailway(ctx, "", nil, append(base, "exec", "--id", sandboxID, "--timeout", "300", "--", "bash", "-lc", script)...)
	if err != nil {
		return railwayOrientation{}, err
	}
	commit := commitPattern.FindString(string(out))
	if commit == "" {
		return railwayOrientation{}, fmt.Errorf("orientation sandbox did not report a repository commit")
	}
	orientation := railwayOrientation{Commit: commit}
	for _, line := range strings.Split(string(out), "\n") {
		if file := strings.TrimSpace(strings.TrimPrefix(line, "VES_FILE ")); strings.HasPrefix(line, "VES_FILE ") && file != "" {
			orientation.Files = append(orientation.Files, file)
		}
	}
	return orientation, nil
}

func orientationCloneRemote(remote string) string {
	trimmed := strings.TrimSpace(remote)
	if strings.HasPrefix(trimmed, "git@github.com:") {
		return "https://github.com/" + strings.TrimPrefix(trimmed, "git@github.com:")
	}
	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme == "ssh" && strings.EqualFold(parsed.Hostname(), "github.com") {
		return "https://github.com/" + strings.TrimPrefix(parsed.Path, "/")
	}
	return trimmed
}
