package controlplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
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
	RailwaySession      *RailwayCLISession
	Logger              *log.Logger
	mu                  sync.Mutex
	promptMu            sync.Mutex
	active              map[string]*sandbox.RailwaySandbox
	activeRuns          map[string]context.CancelFunc
}

func (l *RailwayLauncher) Launch(ctx context.Context, runRecord *state.Run) error {
	return l.launch(ctx, runRecord, "")
}

func (l *RailwayLauncher) LaunchFrom(ctx context.Context, runRecord *state.Run, fromPhase string) error {
	return l.launch(ctx, runRecord, fromPhase)
}

func (l *RailwayLauncher) launch(ctx context.Context, runRecord *state.Run, fromPhase string) error {
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
	var repositoryRecord *state.Repository
	if runRecord.RepositoryID != "" {
		if repository, repositoryErr := l.DB.GetRepository(ctx, runRecord.RepositoryID); repositoryErr != nil {
			return fmt.Errorf("resolve run repository: %w", repositoryErr)
		} else {
			repositoryRecord = repository
			if repository.Remote != "" {
				repositoryRemote = repository.Remote
			}
		}
	}
	if strings.TrimSpace(repositoryRemote) == "" {
		return fmt.Errorf("run repository remote is required")
	}

	branch := fmt.Sprintf("vessica/%s/%s", runRecord.EpicID, runRecord.ID)
	checkpoint, checkpointKind, checkpointReason := l.resolveCheckpoint(repositoryRecord)
	requestedAt := time.Now()
	_, _ = l.DB.AppendEvent(ctx, runRecord.ID, "", "run.infrastructure.stage", map[string]any{
		"stage": "repository_checkpoint_resolve", "duration_ms": 0, "checkpoint": checkpoint,
		"checkpoint_kind": checkpointKind, "cache_hit": checkpointKind == "repository", "reason": checkpointReason,
	})
	if createdAt, parseErr := time.Parse(time.RFC3339Nano, runRecord.CreatedAt); parseErr == nil {
		_, _ = l.DB.AppendEvent(ctx, runRecord.ID, "", "run.infrastructure.stage", map[string]any{
			"stage": "control_plane_queue", "duration_ms": requestedAt.Sub(createdAt).Milliseconds(),
		})
	}
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
		createStarted := time.Now()
		if err := rs.Create(ctx, sandbox.CreateOpts{
			SandboxID: sbRecord.ID, WorkspaceID: sbRecord.WorkspaceID, RunID: runRecord.ID,
			Branch: branch, Env: l.workerEnvironment(runRecord.ID, repositoryRemote, checkpoint, requestedAt), ExpiresAt: retention.EffectiveExpiry(sbRecord),
		}); err != nil {
			_, _ = l.DB.AppendEvent(ctx, runRecord.ID, sbRecord.ID, "run.infrastructure.stage", map[string]any{"stage": "sandbox_create", "duration_ms": time.Since(createStarted).Milliseconds(), "status": "failed", "checkpoint_kind": checkpointKind})
			return err
		}
		_, _ = l.DB.AppendEvent(ctx, runRecord.ID, sbRecord.ID, "run.infrastructure.stage", map[string]any{"stage": "sandbox_create", "duration_ms": time.Since(createStarted).Milliseconds(), "status": "completed", "checkpoint_kind": checkpointKind, "checkpoint": checkpoint})
		sbRecord.ContainerID = rs.ContainerID()
		sbRecord.Status = "running"
		meta, _ := json.Marshal(map[string]any{"railway_sandbox_id": rs.ContainerID(), "branch": branch, "checkpoint": checkpoint, "checkpoint_kind": checkpointKind})
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
	workerURL := strings.TrimRight(l.PublicURL, "/") + "/internal/worker/ves"
	bootstrap := railwayWorkerBootstrap(workerURL, runRecord.ID, fromPhase)
	logPath := filepath.Join(os.TempDir(), "ves-railway-"+runRecord.ID+".log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	writer := io.Writer(os.Stdout)
	if logFile != nil {
		defer logFile.Close()
		writer = io.MultiWriter(os.Stdout, logFile)
	}
	l.Logger.Printf("starting run %s in Railway sandbox %s", runRecord.ID, rs.ContainerID())
	runCtx, cancelRun := context.WithCancel(ctx)
	l.rememberRunCancel(runRecord.ID, cancelRun)
	defer func() {
		cancelRun()
		l.forgetRunCancel(runRecord.ID)
	}()
	workerStarted := time.Now()
	code, execErr := rs.Exec(runCtx, []string{"bash", "-lc", bootstrap}, writer, writer)
	_, _ = l.DB.AppendEvent(context.WithoutCancel(ctx), runRecord.ID, sbRecord.ID, "run.infrastructure.stage", map[string]any{"stage": "worker_process_total", "duration_ms": time.Since(workerStarted).Milliseconds(), "exit_code": code})
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
	if latest.ReceiptID != "" {
		_, _ = receipt.Finalize(ctx, l.DB, latest)
	}
	sbRecord, _ = l.DB.GetSandboxForRun(ctx, runRecord.ID)
	if latest.Preview && sbRecord != nil && sbRecord.PreviewPort > 0 {
		publicPreview, publishErr := l.publishPreview(ctx, rs, latest, sbRecord)
		if publishErr != nil {
			latest.Status = "failed"
			latest.Error = "public_preview_failed: " + publishErr.Error()
			latest.FinishedAt = state.Now()
			_ = l.DB.UpdateRun(ctx, latest)
			_, _ = l.DB.CreateRunEvidence(ctx, latest.ID, "preview", "public_preview", "", "public_preview_failed", map[string]any{"error": publishErr.Error()})
			if latest.ReceiptID != "" {
				_, _ = receipt.Finalize(ctx, l.DB, latest)
			}
			return nil
		}
		latest.PreviewURL = publicPreview
		sbRecord.PreviewURL = publicPreview
		_ = l.DB.UpdateRun(ctx, latest)
		_ = l.DB.UpdateSandbox(ctx, sbRecord)
		if latest.ReceiptID != "" {
			_, _ = receipt.Finalize(ctx, l.DB, latest)
		}
		if latest.PRURL != "" {
			if number, err := repo.ParsePRNumber(latest.PRURL); err == nil {
				_ = repo.UpdatePRBody(ctx, repositoryRemote, number, receipt.PRBody(ctx, l.DB, latest))
			}
		}
	}
	return nil
}

func (l *RailwayLauncher) workerEnvironment(runID, repositoryRemote, checkpoint string, requestedAt time.Time) map[string]string {
	service := "control-plane"
	return map[string]string{
		"VES_RAILWAY_CHECKPOINT":      checkpoint,
		"VES_RUN_ID":                  runID,
		"VES_SANDBOX_REQUESTED_AT_MS": fmt.Sprint(requestedAt.UnixMilli()),
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

func (l *RailwayLauncher) resolveCheckpoint(repository *state.Repository) (name, kind, reason string) {
	base := strings.TrimSpace(l.Config.Hosted.WorkerCheckpoint)
	if repository == nil {
		return base, "toolchain", "repository record unavailable"
	}
	checkpoint, ok := reposnapshot.Parse(repository.MetadataJSON)
	if !ok {
		return base, "toolchain", "repository checkpoint not prepared"
	}
	if !checkpoint.Ready(toolchain.Fingerprint()) {
		return base, "toolchain", "repository checkpoint stale or incompatible"
	}
	return checkpoint.Name, "repository", "ready"
}

func (l *RailwayLauncher) configureAuth(ctx context.Context, rs *sandbox.RailwaySandbox) error {
	if l.RailwaySession != nil && l.RailwaySession.Ready() {
		rs.SetCLIHome(l.RailwaySession.Home)
		rs.SetSessionPersist(func() error {
			persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return l.RailwaySession.Persist(persistCtx)
		})
		return nil
	}
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

func railwayWorkerBootstrap(workerURL, runID, fromPhase string) string {
	command := "control-plane worker --run-id " + shellQuoteCP(runID)
	if strings.TrimSpace(fromPhase) != "" {
		command += " --from " + shellQuoteCP(fromPhase)
	}
	return railwayWorkerBootstrapCommand(workerURL, command)
}

func railwayPromptBootstrap(workerURL, runID, prompt string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(prompt))
	command := "control-plane prompt-worker --run-id " + shellQuoteCP(runID) + " --prompt-b64 " + shellQuoteCP(encoded)
	return railwayWorkerBootstrapCommand(workerURL, command)
}

func railwayWorkerBootstrapCommand(workerURL, workerCommand string) string {
	return strings.Join([]string{
		"set -euo pipefail",
		"export VES_BOOTSTRAP_STARTED_AT_MS=$(date +%s%3N)",
		"export VES_CODEX_EXTERNAL_SANDBOX=1",
		"export VES_RUNNER_USER=vessica-agent",
		"export VES_RUNNER_HOME=/home/vessica-agent",
		"export HOME=/home/vessica-agent",
		"id -u vessica-agent >/dev/null 2>&1 || useradd --create-home --shell /bin/bash vessica-agent",
		"command -v runuser >/dev/null && command -v find >/dev/null && command -v chown >/dev/null && command -v chmod >/dev/null || { echo 'Railway worker checkpoint is missing isolation tools' >&2; exit 1; }",
		"install -d -o vessica-agent -g vessica-agent -m 0700 /home/vessica-agent /home/vessica-agent/.codex",
		"if test -n \"${VES_CODEX_AUTH_B64:-}\"; then auth_b64=$VES_CODEX_AUTH_B64; while test $((${#auth_b64} % 4)) -ne 0; do auth_b64=${auth_b64}=; done; printf '%s' \"$auth_b64\" | base64 -d >/home/vessica-agent/.codex/auth.json; chown vessica-agent:vessica-agent /home/vessica-agent/.codex/auth.json; chmod 0600 /home/vessica-agent/.codex/auth.json; fi",
		"export NPM_CONFIG_PREFIX=/usr/local",
		"export NODE_PATH=/usr/local/lib/node_modules",
		"export PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright",
		"export VES_TOOLCHAIN_VERIFY_STARTED_AT_MS=$(date +%s%3N)",
		toolchain.AgentRuntimeVerifyCommand(),
		"export VES_TOOLCHAIN_VERIFIED_AT_MS=$(date +%s%3N)",
		"if test -n \"${VES_CODEX_AUTH_B64:-}\"; then runuser --user vessica-agent --preserve-environment -- env HOME=/home/vessica-agent codex login status >/dev/null; else test -n \"${OPENAI_API_KEY:-}\" || { echo 'Codex authentication is unavailable' >&2; exit 1; }; fi",
		"export VES_AUTH_VERIFIED_AT_MS=$(date +%s%3N)",
		"worker_bin=$(mktemp /tmp/ves-worker.XXXXXX)",
		"trap 'rm -f \"$worker_bin\"' EXIT",
		"export VES_WORKER_DOWNLOAD_STARTED_AT_MS=$(date +%s%3N)",
		"curl -fsSL -H \"Authorization: Bearer $VES_WORKER_DOWNLOAD_TOKEN\" " + shellQuoteCP(workerURL) + " -o \"$worker_bin\"",
		"chmod +x \"$worker_bin\"",
		"export VES_WORKER_DOWNLOADED_AT_MS=$(date +%s%3N)",
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

func (l *RailwayLauncher) rememberRunCancel(runID string, cancel context.CancelFunc) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeRuns == nil {
		l.activeRuns = map[string]context.CancelFunc{}
	}
	if previous := l.activeRuns[runID]; previous != nil {
		previous()
	}
	l.activeRuns[runID] = cancel
}

func (l *RailwayLauncher) forgetRunCancel(runID string) {
	l.mu.Lock()
	delete(l.activeRuns, runID)
	l.mu.Unlock()
}

func (l *RailwayLauncher) CancelRun(runID string) {
	l.mu.Lock()
	cancel := l.activeRuns[runID]
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
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

// RestorePreviews recreates native Railway forwards after a control-plane
// deployment. The preview process and retained filesystem outlive the server.
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
		if !rs.UsesCLISession() {
			continue
		}
		l.remember(record.RunID, rs)
		forwardURL, err := rs.ExposePort(ctx, record.PreviewPort)
		if err != nil {
			continue
		}
		if err := l.Broker.Register(record.RunID, forwardURL, func() { _ = rs.StopForward() }); err != nil {
			_ = rs.StopForward()
			continue
		}
		publicURL := ""
		if parsed, err := url.Parse(record.PreviewURL); err == nil && parsed.Query().Get("cap") != "" {
			capability := parsed.Query().Get("cap")
			_ = l.Broker.RestoreCapability(capability, record.RunID)
			publicURL, err = l.publicPreviewURL(record.RunID, capability)
			if err != nil {
				l.Broker.Remove(record.RunID)
				continue
			}
		} else {
			published, publishErr := l.issuePublicPreviewURL(record.RunID)
			if publishErr != nil {
				l.Broker.Remove(record.RunID)
				continue
			}
			publicURL = published
		}
		if err := waitForPublicPreview(ctx, publicURL, 60*time.Second); err != nil {
			continue
		}
		record.PreviewURL = publicURL
		_ = l.DB.UpdateSandbox(ctx, record)
		if runRecord, err := l.DB.GetRun(ctx, record.RunID); err == nil {
			runRecord.PreviewURL = publicURL
			_ = l.DB.UpdateRun(ctx, runRecord)
			if runRecord.ReceiptID != "" {
				_, _ = receipt.Finalize(ctx, l.DB, runRecord)
			}
			if runRecord.PRURL != "" {
				if repository, repositoryErr := l.DB.GetRepository(ctx, runRecord.RepositoryID); repositoryErr == nil {
					if number, parseErr := repo.ParsePRNumber(runRecord.PRURL); parseErr == nil {
						_ = repo.UpdatePRBody(ctx, repository.Remote, number, receipt.PRBody(ctx, l.DB, runRecord))
					}
				}
			}
		}
	}
}

var _ RunLauncher = (*RailwayLauncher)(nil)
