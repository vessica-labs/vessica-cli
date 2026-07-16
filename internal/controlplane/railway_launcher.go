package controlplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/sandbox"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

type RailwayLauncher struct {
	DB                  *state.DB
	Config              config.Config
	CLIPath             string
	PublicURL           string
	PreviewPublicURL    string
	WorkerDownloadToken string
	Broker              *PreviewBroker
	RailwayToken        func(context.Context) (string, error)
	Logger              *log.Logger
	mu                  sync.Mutex
	promptMu            sync.Mutex
	active              map[string]*sandbox.RailwaySandbox
}

func (l *RailwayLauncher) Launch(ctx context.Context, runRecord *state.Run) error {
	if l.DB == nil || runRecord == nil {
		return fmt.Errorf("railway launcher requires a database and run")
	}
	if l.Config.Hosted.ProjectID == "" || l.Config.Hosted.EnvironmentID == "" {
		return fmt.Errorf("railway project and environment are required")
	}
	if l.Broker == nil {
		l.Broker = NewPreviewBroker()
	}
	if l.Logger == nil {
		l.Logger = log.New(os.Stdout, "railway-launcher ", log.LstdFlags|log.LUTC)
	}
	repositoryRemote := l.Config.Repo.Remote
	if runRecord.RepositoryID != "" {
		if repository, repositoryErr := l.DB.GetRepository(ctx, runRecord.RepositoryID); repositoryErr != nil {
			return fmt.Errorf("resolve run repository: %w", repositoryErr)
		} else if repository.Remote != "" {
			repositoryRemote = repository.Remote
		}
	}
	if strings.TrimSpace(repositoryRemote) == "" {
		return fmt.Errorf("run repository remote is required")
	}

	branch := fmt.Sprintf("vessica/%s/%s", runRecord.EpicID, runRecord.ID)
	sbRecord, err := l.DB.GetSandboxForRun(ctx, runRecord.ID)
	if err != nil || sbRecord.Backend != "railway" || sbRecord.ContainerID == "" {
		sbRecord, err = l.DB.CreateSandbox(ctx, runRecord.ID, "railway", branch)
		if err != nil {
			return err
		}
		if err := retention.Initialize(ctx, l.DB, sbRecord); err != nil {
			return err
		}
		rs := sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, "")
		if err := l.configureAuth(ctx, rs); err != nil {
			return err
		}
		if err := rs.Create(ctx, sandbox.CreateOpts{
			SandboxID: sbRecord.ID, WorkspaceID: sbRecord.WorkspaceID, RunID: runRecord.ID,
			Branch: branch, Env: l.workerEnvironment(runRecord.ID, repositoryRemote), ExpiresAt: retention.EffectiveExpiry(sbRecord),
		}); err != nil {
			return err
		}
		sbRecord.ContainerID = rs.ContainerID()
		sbRecord.Status = "running"
		meta, _ := json.Marshal(map[string]any{"railway_sandbox_id": rs.ContainerID(), "branch": branch})
		sbRecord.MetaJSON = string(meta)
		if err := l.DB.UpdateSandbox(ctx, sbRecord); err != nil {
			return err
		}
		l.remember(runRecord.ID, rs)
	} else {
		rs := sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, sbRecord.ContainerID)
		if err := l.configureAuth(ctx, rs); err != nil {
			return err
		}
		l.remember(runRecord.ID, rs)
	}

	rs := l.lookup(runRecord.ID)
	if err := l.configureIdentity(rs); err != nil {
		return err
	}
	workerURL := strings.TrimRight(l.PublicURL, "/") + "/internal/worker/ves"
	bootstrap := railwayWorkerBootstrap(workerURL, runRecord.ID)
	logPath := filepath.Join(os.TempDir(), "ves-railway-"+runRecord.ID+".log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	writer := io.Writer(os.Stdout)
	if logFile != nil {
		defer logFile.Close()
		writer = io.MultiWriter(os.Stdout, logFile)
	}
	l.Logger.Printf("starting run %s in Railway sandbox %s", runRecord.ID, rs.ContainerID())
	code, execErr := rs.Exec(ctx, []string{"bash", "-lc", bootstrap}, writer, writer)
	latest, getErr := l.DB.GetRun(ctx, runRecord.ID)
	if getErr != nil {
		return getErr
	}
	if execErr != nil || code != 0 {
		return fmt.Errorf("railway worker exited %d: %w", code, execErr)
	}
	if latest.Status != "completed" {
		return fmt.Errorf("railway worker finished with run status %s: %s", latest.Status, latest.Error)
	}
	sbRecord, _ = l.DB.GetSandboxForRun(ctx, runRecord.ID)
	if latest.Preview && sbRecord != nil && sbRecord.PreviewPort > 0 {
		forwardCtx, cancel := context.WithCancel(context.Background())
		target, err := rs.ExposePort(forwardCtx, sbRecord.PreviewPort)
		if err != nil {
			cancel()
			return err
		}
		if err := l.Broker.Register(runRecord.ID, target, cancel); err != nil {
			cancel()
			return err
		}
		previewBase := l.PreviewPublicURL
		if previewBase == "" {
			previewBase = l.PublicURL
		}
		publicPreview := strings.TrimRight(previewBase, "/") + "/previews/" + runRecord.ID + "/"
		latest.PreviewURL = publicPreview
		sbRecord.PreviewURL = publicPreview
		_ = l.DB.UpdateRun(ctx, latest)
		_ = l.DB.UpdateSandbox(ctx, sbRecord)
		if latest.PRURL != "" {
			if number, err := repo.ParsePRNumber(latest.PRURL); err == nil {
				_ = repo.UpdatePRBody(ctx, repositoryRemote, number, receipt.PRBody(ctx, l.DB, latest))
			}
		}
	}
	return nil
}

func (l *RailwayLauncher) workerEnvironment(runID, repositoryRemote string) map[string]string {
	service := "control-plane"
	return map[string]string{
		"VES_RAILWAY_CHECKPOINT":      l.Config.Hosted.WorkerCheckpoint,
		"VES_RUN_ID":                  runID,
		"VES_CONTROL_DATABASE_URL":    service + ".VES_CONTROL_DATABASE_URL",
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
		"VES_RUNNER_USER":             "vessica-agent",
		"VES_RUNNER_HOME":             "/home/vessica-agent",
		"GITHUB_TOKEN":                service + ".GITHUB_TOKEN",
		"OPENAI_API_KEY":              service + ".OPENAI_API_KEY",
		"VES_CODEX_AUTH_B64":          service + ".VES_CODEX_AUTH_B64",
	}
}

func (l *RailwayLauncher) configureAuth(ctx context.Context, rs *sandbox.RailwaySandbox) error {
	if l.RailwayToken == nil {
		return nil
	}
	token, err := l.RailwayToken(ctx)
	if err != nil {
		return fmt.Errorf("get Railway access token: %w", err)
	}
	rs.SetAPIToken(token)
	return nil
}

func railwayWorkerBootstrap(workerURL, runID string) string {
	return railwayWorkerBootstrapCommand(workerURL, "control-plane worker --run-id "+shellQuoteCP(runID))
}

func railwayPromptBootstrap(workerURL, runID, prompt string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(prompt))
	command := "control-plane prompt-worker --run-id " + shellQuoteCP(runID) + " --prompt-b64 " + shellQuoteCP(encoded)
	return railwayWorkerBootstrapCommand(workerURL, command)
}

func railwayWorkerBootstrapCommand(workerURL, workerCommand string) string {
	return strings.Join([]string{
		"set -euo pipefail",
		"export VES_CODEX_EXTERNAL_SANDBOX=1",
		"export VES_RUNNER_USER=vessica-agent",
		"export VES_RUNNER_HOME=/home/vessica-agent",
		"id -u vessica-agent >/dev/null 2>&1 || useradd --create-home --shell /bin/bash vessica-agent",
		"command -v runuser >/dev/null && command -v find >/dev/null && command -v chown >/dev/null && command -v chmod >/dev/null || { echo 'Railway worker checkpoint is missing isolation tools' >&2; exit 1; }",
		"install -d -o vessica-agent -g vessica-agent -m 0700 /home/vessica-agent /home/vessica-agent/.codex",
		"export NODE_PATH=$(npm root -g)",
		"export PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright",
		toolchain.AgentShellVerifyCommand(),
		"worker_bin=$(mktemp /tmp/ves-worker.XXXXXX)",
		"trap 'rm -f \"$worker_bin\"' EXIT",
		"curl -fsSL -H \"Authorization: Bearer $VES_WORKER_DOWNLOAD_TOKEN\" " + shellQuoteCP(workerURL) + " -o \"$worker_bin\"",
		"chmod +x \"$worker_bin\"",
		"\"$worker_bin\" " + workerCommand,
	}, "\n")
}

func (l *RailwayLauncher) Prompt(ctx context.Context, runRecord *state.Run, prompt string) (*runengine.PromptResult, error) {
	l.promptMu.Lock()
	defer l.promptMu.Unlock()
	if l.Logger == nil {
		l.Logger = log.New(os.Stdout, "railway-launcher ", log.LstdFlags|log.LUTC)
	}
	if runRecord == nil || strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("run and prompt are required")
	}
	sandboxRecord, err := l.DB.GetSandboxForRun(ctx, runRecord.ID)
	if err != nil {
		return nil, err
	}
	if sandboxRecord.ContainerID == "" || sandboxRecord.Status == "destroyed" || sandboxRecord.Status == "expired" {
		return nil, fmt.Errorf("preview sandbox is no longer available")
	}
	rs := l.lookup(runRecord.ID)
	if rs == nil {
		rs = sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, sandboxRecord.ContainerID)
		if err := l.configureAuth(ctx, rs); err != nil {
			return nil, err
		}
		l.remember(runRecord.ID, rs)
	}
	if err := l.configureIdentity(rs); err != nil {
		return nil, err
	}
	workerURL := strings.TrimRight(l.PublicURL, "/") + "/internal/worker/ves"
	bootstrap := railwayPromptBootstrap(workerURL, runRecord.ID, prompt)
	var output bytes.Buffer
	writer := io.MultiWriter(os.Stdout, &output)
	l.Logger.Printf("prompting run %s in Railway sandbox %s", runRecord.ID, rs.ContainerID())
	code, execErr := rs.Exec(ctx, []string{"bash", "-lc", bootstrap}, writer, writer)
	if execErr != nil || code != 0 {
		return nil, fmt.Errorf("railway prompt worker exited %d: %w", code, execErr)
	}
	const marker = "VES_PROMPT_RESULT:"
	index := strings.LastIndex(output.String(), marker)
	if index < 0 {
		return nil, fmt.Errorf("railway prompt worker returned no result")
	}
	line := strings.SplitN(output.String()[index+len(marker):], "\n", 2)[0]
	var result runengine.PromptResult
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		return nil, fmt.Errorf("decode railway prompt result: %w", err)
	}
	return &result, nil
}

func shellQuoteCP(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func (l *RailwayLauncher) remember(runID string, rs *sandbox.RailwaySandbox) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active == nil {
		l.active = map[string]*sandbox.RailwaySandbox{}
	}
	l.active[runID] = rs
}

func (l *RailwayLauncher) lookup(runID string) *sandbox.RailwaySandbox {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active[runID]
}

func (l *RailwayLauncher) Destroy(ctx context.Context, record *state.Sandbox) error {
	if record == nil {
		return nil
	}
	if l.Broker != nil {
		l.Broker.Remove(record.RunID)
	}
	rs := l.lookup(record.RunID)
	if rs == nil {
		rs = sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, record.ContainerID)
	}
	if err := l.configureAuth(ctx, rs); err != nil {
		return err
	}
	if err := rs.Destroy(ctx); err != nil {
		return err
	}
	record.Status = "destroyed"
	record.PreviewURL = ""
	record.DestroyedAt = state.Now()
	return l.DB.UpdateSandbox(ctx, record)
}

// RestorePreviews recreates forward sessions after a control-plane deployment
// restart. Railway sandboxes and their filesystem state outlive the service
// process; only the localhost forwarding session needs to be rebuilt.
func (l *RailwayLauncher) RestorePreviews(ctx context.Context) {
	if l.Broker == nil || l.DB == nil {
		return
	}
	records, err := l.DB.ListSandboxes(ctx)
	if err != nil {
		return
	}
	for i := range records {
		record := &records[i]
		if record.Backend != "railway" || record.ContainerID == "" || record.PreviewPort <= 0 || record.Status == "destroyed" || record.Status == "expired" {
			continue
		}
		if expiry := retention.EffectiveExpiry(record); !expiry.IsZero() && time.Now().After(expiry) {
			_ = l.Destroy(ctx, record)
			continue
		}
		rs := sandbox.NewRailway(l.CLIPath, l.Config.Hosted.ProjectID, l.Config.Hosted.EnvironmentID, record.ContainerID)
		if err := l.configureAuth(ctx, rs); err != nil {
			continue
		}
		if err := l.configureIdentity(rs); err != nil {
			continue
		}
		forwardCtx, cancel := context.WithCancel(context.Background())
		target, err := rs.ExposePort(forwardCtx, record.PreviewPort)
		if err != nil {
			cancel()
			continue
		}
		l.remember(record.RunID, rs)
		_ = l.Broker.Register(record.RunID, target, cancel)
		previewBase := l.PreviewPublicURL
		if previewBase == "" {
			previewBase = l.PublicURL
		}
		publicURL := strings.TrimRight(previewBase, "/") + "/previews/" + record.RunID + "/"
		record.PreviewURL = publicURL
		_ = l.DB.UpdateSandbox(ctx, record)
		if runRecord, err := l.DB.GetRun(ctx, record.RunID); err == nil {
			runRecord.PreviewURL = publicURL
			_ = l.DB.UpdateRun(ctx, runRecord)
		}
	}
}

func (l *RailwayLauncher) configureIdentity(rs *sandbox.RailwaySandbox) error {
	if rs == nil {
		return fmt.Errorf("Railway sandbox is required")
	}
	privateKey := strings.TrimSpace(os.Getenv("VES_RAILWAY_SSH_PRIVATE_KEY"))
	if privateKey == "" {
		return fmt.Errorf("VES_RAILWAY_SSH_PRIVATE_KEY is required for Railway preview forwarding")
	}
	path := filepath.Join(os.TempDir(), "vessica-railway-ed25519")
	if err := os.WriteFile(path, []byte(privateKey+"\n"), 0o600); err != nil {
		return err
	}
	rs.SetIdentityFile(path)
	return nil
}

var _ RunLauncher = (*RailwayLauncher)(nil)
