package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
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

func installationCandidateSummary(candidates []railwayInstallationCandidate) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, candidate.ProjectID+" ("+candidate.Endpoint+")")
	}
	return strings.Join(items, ", ")
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

func railwayWorkspaceName(selector string, workspaces []railwayWorkspaceChoice) string {
	for _, workspace := range workspaces {
		if selector == workspace.ID || strings.EqualFold(selector, workspace.Name) {
			return workspace.Name
		}
	}
	return ""
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
	cfg.Hosted = config.HostedConfig{Provider: "railway", WorkspaceID: candidate.WorkspaceID, WorkspaceName: candidate.WorkspaceName, ProjectID: candidate.ProjectID, EnvironmentID: candidate.EnvironmentID, ServiceID: candidate.ServiceID, ControlPlaneURL: candidate.Endpoint}
	if err := reconcileRailwayResourceIDs(ctx, &cfg); err != nil {
		return cfg, railwaySecrets{}, err
	}
	controlVariables, err := readRailwayVariables(ctx, cfg, cfg.Hosted.ServiceID)
	if err != nil {
		return cfg, railwaySecrets{}, err
	}
	cfg.Hosted.WorkerCheckpoint = controlVariables["VES_RAILWAY_CHECKPOINT"]
	cfg.Hosted.PreviewURL = controlVariables["VES_PREVIEW_ORIGIN"]
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
		PreviewEdgeToken:          controlVariables["VES_PREVIEW_EDGE_TOKEN"],
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
	Commit     string
	Files      []string
	Checkpoint reposnapshot.Checkpoint
	Timings    map[string]int64
}

func runRailwayOrientation(ctx context.Context, cfg config.Config, remote string) (railwayOrientation, error) {
	cloneRemote := orientationCloneRemote(remote)
	base := []string{"sandbox", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID}
	args := append(append([]string{}, base...), "create", "--checkpoint", cfg.Hosted.WorkerCheckpoint, "--private-network", "--idle-timeout-minutes", "20", "--variable", "VES_REPO_REMOTE="+cloneRemote, "--variable", "GITHUB_TOKEN=control-plane.GITHUB_TOKEN", "--variable", "VES_CONTROL_PLANE_URL="+cfg.Hosted.ControlPlaneURL, "--variable", "VES_WORKER_DOWNLOAD_TOKEN=control-plane.VES_WORKER_DOWNLOAD_TOKEN", "--json")
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
orientation_started=$(date +%s%3N)
askpass=$(mktemp)
trap 'rm -f "$askpass"' EXIT
cat >"$askpass" <<'EOF'
#!/usr/bin/env bash
case "$1" in *Username*) printf '%s\n' x-access-token ;; *) printf '%s\n' "$GITHUB_TOKEN" ;; esac
EOF
chmod 0700 "$askpass"
rm -rf /workspace/repo /workspace/.vessica-repository-checkpoint.json
clone_started=$(date +%s%3N)
GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 git clone --quiet --depth=1 "$VES_REPO_REMOTE" /workspace/repo
git -C /workspace/repo remote set-url origin "$VES_REPO_REMOTE"
commit=$(git -C /workspace/repo rev-parse HEAD)
clone_finished=$(date +%s%3N)
rm -f "$askpass"
if test -n "${VES_CONTROL_PLANE_URL:-}" && test -n "${VES_WORKER_DOWNLOAD_TOKEN:-}"; then
  install -d -m 0755 /opt/vessica/bin
  worker_url="${VES_CONTROL_PLANE_URL%/}/internal/worker/ves"
  worker_digest=$(curl -fsSI -H "Authorization: Bearer $VES_WORKER_DOWNLOAD_TOKEN" "$worker_url" | awk -F': ' 'tolower($1)=="x-vessica-worker-sha256" {gsub(/\r/,"",$2); print $2}')
  worker_tmp=$(mktemp)
  curl -fsSL -H "Authorization: Bearer $VES_WORKER_DOWNLOAD_TOKEN" "$worker_url" -o "$worker_tmp"
  echo "$worker_digest  $worker_tmp" | sha256sum -c -
  install -m 0755 "$worker_tmp" /opt/vessica/bin/ves-worker
  printf '%s\n' "$worker_digest" >/opt/vessica/bin/ves-worker.sha256
  rm -f "$worker_tmp"
fi
unset GITHUB_TOKEN VES_REPO_REMOTE VES_CONTROL_PLANE_URL VES_WORKER_DOWNLOAD_TOKEN

dependency_fingerprint=$(python3 - <<'PY'
import hashlib, os
root = "/workspace/repo"
names = "package.json pnpm-lock.yaml package-lock.json yarn.lock go.mod go.sum pyproject.toml uv.lock poetry.lock requirements.txt Cargo.toml Cargo.lock Gemfile Gemfile.lock pom.xml build.gradle build.gradle.kts gradle.properties composer.json composer.lock".split()
h = hashlib.sha256()
found = False
for name in names:
    path = os.path.join(root, name)
    if not os.path.isfile(path):
        continue
    found = True
    h.update(name.encode() + b"\0")
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            h.update(chunk)
if not found:
    h.update(b"no-dependency-manifest")
print(h.hexdigest())
PY
)
stack=generic
stacks=()
dependency_state=no_manifest
install -d -o vessica-agent -g vessica-agent -m 0755 /workspace/repo
chown -R vessica-agent:vessica-agent /workspace/repo
run_dependency() {
  if ! runuser --user vessica-agent --preserve-environment -- env HOME=/home/vessica-agent bash -lc "$1"; then
    dependency_state=deferred
  fi
}
dependency_started=$(date +%s%3N)
if test -f /workspace/repo/package.json; then
	stacks+=(node)
	dependency_state=ready
  if test -f /workspace/repo/pnpm-lock.yaml; then
    run_dependency 'cd /workspace/repo && pnpm install --frozen-lockfile'
  elif test -f /workspace/repo/package-lock.json; then
    run_dependency 'cd /workspace/repo && npm ci'
  elif test -f /workspace/repo/yarn.lock; then
    run_dependency 'cd /workspace/repo && corepack yarn install --immutable'
	else
		run_dependency 'cd /workspace/repo && npm install --no-package-lock'
	fi
fi
if test -f /workspace/repo/go.mod; then
	stacks+=(go)
	dependency_state=ready
	run_dependency 'cd /workspace/repo && go mod download'
fi
if test -f /workspace/repo/pyproject.toml || test -f /workspace/repo/requirements.txt; then
	stacks+=(python)
	dependency_state=ready
	run_dependency 'cd /workspace/repo && python3 -m venv .venv && if test -f requirements.txt; then .venv/bin/pip install -r requirements.txt; else .venv/bin/pip install -e .; fi'
fi
if test -f /workspace/repo/Cargo.toml; then
	stacks+=(rust)
	dependency_state=ready
  command -v cargo >/dev/null || { apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends cargo rustc && rm -rf /var/lib/apt/lists/*; }
	run_dependency 'cd /workspace/repo && cargo fetch --locked'
fi
if test -f /workspace/repo/Gemfile; then
	stacks+=(ruby)
  dependency_state=ready
  command -v bundle >/dev/null || { apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ruby-full bundler && rm -rf /var/lib/apt/lists/*; }
	run_dependency 'cd /workspace/repo && bundle config set path vendor/bundle && bundle install'
fi
if test -f /workspace/repo/pom.xml || test -f /workspace/repo/build.gradle || test -f /workspace/repo/build.gradle.kts; then
	stacks+=(java)
  dependency_state=ready
  command -v java >/dev/null || { apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends default-jdk maven gradle && rm -rf /var/lib/apt/lists/*; }
	run_dependency 'cd /workspace/repo && if test -x ./gradlew; then ./gradlew dependencies --no-daemon; elif test -f pom.xml; then mvn -q dependency:go-offline; else gradle dependencies --no-daemon; fi'
fi
if test ${#stacks[@]} -gt 0; then stack=$(IFS=+; echo "${stacks[*]}"); fi
dependency_finished=$(date +%s%3N)

jq -n --arg commit "$commit" --arg dependency_fingerprint "$dependency_fingerprint" --arg stack "$stack" --arg dependency_state "$dependency_state" '{schema_version:1,base_commit:$commit,dependency_fingerprint:$dependency_fingerprint,stack:$stack,dependency_state:$dependency_state}' >/workspace/.vessica-repository-checkpoint.json
chmod 0644 /workspace/.vessica-repository-checkpoint.json
printf '%s\n' "$commit"
printf 'VES_DEPENDENCY_FINGERPRINT %s\n' "$dependency_fingerprint"
printf 'VES_STACK %s\n' "$stack"
printf 'VES_DEPENDENCY_STATE %s\n' "$dependency_state"
printf 'VES_TIMING clone_ms=%s dependency_ms=%s total_ms=%s\n' "$((clone_finished-clone_started))" "$((dependency_finished-dependency_started))" "$((dependency_finished-orientation_started))"
find /workspace/repo -maxdepth 3 -type f -not -path '*/.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' -not -path '*/.venv/*' | sed 's#^/workspace/repo/##' | sort | head -500 | sed 's#^#VES_FILE #'
`
	out, err := runRailway(ctx, "", nil, append(base, "exec", "--id", sandboxID, "--timeout", "300", "--", "bash", "-lc", script)...)
	if err != nil {
		return railwayOrientation{}, err
	}
	commit := commitPattern.FindString(string(out))
	if commit == "" {
		return railwayOrientation{}, fmt.Errorf("orientation sandbox did not report a repository commit")
	}
	orientation := railwayOrientation{Commit: commit, Timings: map[string]int64{}}
	for _, line := range strings.Split(string(out), "\n") {
		if file := strings.TrimSpace(strings.TrimPrefix(line, "VES_FILE ")); strings.HasPrefix(line, "VES_FILE ") && file != "" {
			orientation.Files = append(orientation.Files, file)
		}
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "VES_DEPENDENCY_FINGERPRINT ") && len(fields) == 2:
			orientation.Checkpoint.DependencyFingerprint = fields[1]
		case strings.HasPrefix(line, "VES_STACK ") && len(fields) == 2:
			orientation.Checkpoint.Stack = fields[1]
		case strings.HasPrefix(line, "VES_DEPENDENCY_STATE ") && len(fields) == 2:
			orientation.Checkpoint.DependencyState = fields[1]
		case strings.HasPrefix(line, "VES_TIMING "):
			for _, value := range fields[1:] {
				parts := strings.SplitN(value, "=", 2)
				if len(parts) == 2 {
					orientation.Timings[parts[0]], _ = strconv.ParseInt(parts[1], 10, 64)
				}
			}
		}
	}
	if orientation.Checkpoint.DependencyFingerprint == "" || orientation.Checkpoint.Stack == "" {
		return railwayOrientation{}, fmt.Errorf("orientation sandbox did not report its dependency contract")
	}
	orientation.Checkpoint.SchemaVersion = reposnapshot.SchemaVersion
	orientation.Checkpoint.Specification, orientation.Checkpoint.SpecificationFingerprint = reposnapshot.InferSpecification(orientation.Files, orientation.Checkpoint.Stack)
	orientation.Checkpoint.Name = reposnapshot.Name(state.CanonicalRepositoryRemote(remote), commit, orientation.Checkpoint.DependencyFingerprint, orientation.Checkpoint.SpecificationFingerprint, toolchain.Fingerprint())
	orientation.Checkpoint.Status = "ready"
	orientation.Checkpoint.BaseCommit = commit
	orientation.Checkpoint.ToolchainFingerprint = toolchain.Fingerprint()
	orientation.Checkpoint.PreparedAt = time.Now().UTC().Format(time.RFC3339Nano)
	orientation.Checkpoint.VerifiedAt = orientation.Checkpoint.PreparedAt
	orientation.Checkpoint.Verification = "orientation_dependency_install"
	list, listErr := runRailway(ctx, "", nil, append(base, "checkpoint", "list", "--json")...)
	if listErr != nil || !bytes.Contains(list, []byte(orientation.Checkpoint.Name)) {
		checkpointJSON, _ := json.Marshal(orientation.Checkpoint)
		marker := "printf '%s' " + strconv.Quote(base64.StdEncoding.EncodeToString(checkpointJSON)) + " | base64 -d >/workspace/.vessica-repository-checkpoint.json && chmod 0644 /workspace/.vessica-repository-checkpoint.json"
		if _, err := runRailway(ctx, "", nil, append(base, "exec", "--id", sandboxID, "--timeout", "60", "--", "bash", "-lc", marker)...); err != nil {
			return railwayOrientation{}, err
		}
		captureStarted := time.Now()
		if _, err := runRailway(ctx, "", nil, append(base, "checkpoint", "create", orientation.Checkpoint.Name, "--id", sandboxID, "--json")...); err != nil {
			return railwayOrientation{}, err
		}
		orientation.Timings["checkpoint_capture_ms"] = time.Since(captureStarted).Milliseconds()
	} else {
		orientation.Timings["checkpoint_cache_hit"] = 1
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

func resumeRequiresRepositoryMap(resumeID string, operation *onboarding.Operation) bool {
	if strings.TrimSpace(resumeID) == "" || operation == nil || operation.CurrentStage != "repository_mapping" {
		return false
	}
	for _, stage := range operation.Stages {
		if stage.Name == "repository_mapping" {
			return stage.Status == "failed" || stage.Status == "running"
		}
	}
	return false
}
