package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/repo"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (e *Engine) prepareTicketWorktree(ctx context.Context, integrationWorkdir string, r *state.Run, ticket *state.Ticket) (string, string, error) {
	branch := fmt.Sprintf("vessica/%s/%s-%s", r.EpicID, r.ID, ticket.ID)
	base := filepath.Join(filepath.Dir(integrationWorkdir), "tickets")
	workdir := filepath.Join(base, ticket.ID)
	_ = repo.GitCommandContext(ctx, "-C", integrationWorkdir, "worktree", "remove", "--force", workdir).Run()
	_ = os.RemoveAll(workdir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", err
	}
	out, err := repo.GitCommandContext(ctx, "-C", integrationWorkdir, "worktree", "add", "-B", branch, workdir, "HEAD").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add for ticket %s: %w: %s", ticket.ID, err, strings.TrimSpace(string(out)))
	}
	started := time.Now()
	if err := isolation.PrepareWorkdir(ctx, workdir); err != nil {
		return "", "", err
	}
	if err := isolation.TrustGitWorkdir(ctx, workdir); err != nil {
		return "", "", err
	}
	e.emit(ctx, r.ID, "run.infrastructure.stage", map[string]any{
		"stage":       "git_worktree_trust",
		"ticket_id":   ticket.ID,
		"duration_ms": time.Since(started).Milliseconds(),
		"scope":       "exact",
		"status":      "completed",
	})
	return workdir, branch, nil
}

func (e *Engine) commitTicketWork(ctx context.Context, workdir, ticketID, title string) (string, []string, error) {
	out, err := repo.GitCommandContext(ctx, "-C", workdir, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("git status: %w: %s", err, strings.TrimSpace(string(out)))
	}
	files := statusFiles(string(out))
	if len(files) == 0 && !simulationMode() {
		return "", nil, fmt.Errorf("ticket %s produced no file changes", ticketID)
	}
	if _, err := repo.GitCommandContext(ctx, "-C", workdir, "add", "-A").CombinedOutput(); err != nil {
		return "", nil, err
	}
	args := []string{"-C", workdir, "commit", "-m", fmt.Sprintf("ves: %s %s", ticketID, title)}
	if simulationMode() && len(files) == 0 {
		args = append(args, "--allow-empty")
	}
	if out, err := repo.GitCommandContext(ctx, args...).CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("git commit ticket %s: %w: %s", ticketID, err, strings.TrimSpace(string(out)))
	}
	sha, err := repo.GitCommandContext(ctx, "-C", workdir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", files, err
	}
	return strings.TrimSpace(string(sha)), files, nil
}

func (e *Engine) mergeTicketBranch(ctx context.Context, integrationWorkdir, branch, runID, ticketID string) error {
	e.emit(ctx, runID, "merge.started", map[string]any{"ticket_id": ticketID, "branch": branch})
	out, err := repo.GitCommandContext(ctx, "-C", integrationWorkdir, "merge", "--no-ff", "--no-edit", branch).CombinedOutput()
	if err != nil {
		_, _ = repo.GitCommandContext(ctx, "-C", integrationWorkdir, "merge", "--abort").CombinedOutput()
		_, _ = e.DB.CreateRunEvidence(ctx, runID, "code", "merge", ticketID, "failed", map[string]any{"branch": branch, "output": redaction.Redact(string(out)), "error": err.Error()})
		e.emit(ctx, runID, "merge.failed", map[string]any{"ticket_id": ticketID, "branch": branch, "message": redaction.Redact(string(out))})
		return fmt.Errorf("merge ticket %s: %w", ticketID, err)
	}
	_, _ = e.DB.CreateRunEvidence(ctx, runID, "code", "merge", ticketID, "passed", map[string]any{"branch": branch, "output": truncate(redaction.Redact(string(out)), 2000)})
	e.emit(ctx, runID, "merge.completed", map[string]any{"ticket_id": ticketID, "branch": branch})
	return nil
}

func statusFiles(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) >= 4 {
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}
	return files
}

func scenarioSteps(markdown string) []string {
	var steps []string
	for _, line := range strings.Split(markdown, "\n") {
		s := strings.TrimSpace(line)
		s = strings.TrimPrefix(s, "- [ ]")
		s = strings.TrimPrefix(s, "- [x]")
		s = strings.TrimPrefix(s, "-")
		if len(s) > 2 && s[1] == '.' && s[0] >= '0' && s[0] <= '9' {
			s = strings.TrimSpace(s[2:])
		}
		if len(s) > 3 && s[2] == '.' && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' {
			s = strings.TrimSpace(s[3:])
		}
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		lower := strings.ToLower(s)
		if strings.Contains(lower, "scenario") || strings.Contains(lower, "path") || strings.Contains(lower, "works") || strings.Contains(lower, "loads") || strings.Contains(lower, "handled") || strings.Contains(lower, "green") || strings.Contains(lower, "regression") {
			steps = append(steps, s)
		}
	}
	return steps
}

func ensurePlaywright(ctx context.Context, workdir string) error {
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node is required for Playwright validation")
	}
	cmd := isolation.CommandContext(ctx, workdir, "node", "-e", "require('playwright');")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("playwright package is required for validation: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runPlaywrightStep(ctx context.Context, workdir, url, step string) error {
	script := `
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', e => errors.push(String(e)));
  page.on('response', r => { if (r.status() >= 500) errors.push(r.url() + ' ' + r.status()); });
  await page.goto(process.argv[2], { waitUntil: 'domcontentloaded', timeout: 15000 });
  await page.waitForLoadState('networkidle', { timeout: 10000 }).catch(() => {});
  const title = await page.title().catch(() => '');
  const body = await page.locator('body').innerText({ timeout: 5000 }).catch(() => '');
  await browser.close();
  if (errors.length) throw new Error(errors.join('\n'));
  if (!title && !body.trim()) throw new Error('page rendered no visible content');
})().catch(err => { console.error(err.message || err); process.exit(1); });
`
	tmp, err := os.CreateTemp("", "ves-playwright-*.js")
	if err != nil {
		return err
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return err
	}
	_ = tmp.Close()
	if err := os.Chmod(path, 0o644); err != nil {
		_ = os.Remove(path)
		return err
	}
	defer os.Remove(path)
	cmd := isolation.CommandContext(ctx, workdir, "node", path, url, step)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", step, err, strings.TrimSpace(string(out)))
	}
	return nil
}
