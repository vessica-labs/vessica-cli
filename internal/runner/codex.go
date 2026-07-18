package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/isolation"
)

// CodexRunner invokes the Codex CLI when available; otherwise uses a deterministic local planner.
type CodexRunner struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	events       chan Event
	result       Result
	input        Input
	useStub      bool
	missing      bool
	mcpPolicy    string
	mcpDisabled  int
	mcpDiscovery string
}

type codexMCPServer struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

var codexMCPServerCache = struct {
	sync.Mutex
	servers map[string][]codexMCPServer
	sources map[string]string
}{servers: map[string][]codexMCPServer{}, sources: map[string]string{}}

func NewCodex() *CodexRunner {
	_, err := exec.LookPath("codex")
	useStub := os.Getenv("VES_RUNNER_MODE") == "stub" || os.Getenv("VES_SIMULATION") == "1"
	return &CodexRunner{useStub: useStub, missing: err != nil}
}

func (c *CodexRunner) Name() string { return "codex" }

func (c *CodexRunner) Prepare(ctx context.Context, in Input) error {
	c.input = in
	c.events = make(chan Event, 64)
	return nil
}

func (c *CodexRunner) Start(ctx context.Context, task Task) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	prompt := strings.TrimSpace(task.SystemPrompt + "\n\n" + task.Prompt)
	if c.input.Instructions != "" {
		prompt += "\n\n## Instructions\n" + c.input.Instructions
	}
	if c.input.TicketContext != "" {
		prompt += "\n\n## Ticket\n" + c.input.TicketContext
	}
	if c.input.ArtifactContext != "" {
		prompt += "\n\n## Artifacts\n" + c.input.ArtifactContext
	}

	if c.useStub || c.input.AllowStub {
		go c.runStub(ctx, task, prompt)
		return nil
	}
	if c.missing {
		return fmt.Errorf("codex runner not found in PATH; set VES_RUNNER_MODE=stub for simulation")
	}

	runCtx, cancel := context.WithTimeout(ctx, runnerTimeout())
	c.cancel = cancel
	outFile := filepath.Join(os.TempDir(), fmt.Sprintf("ves-codex-%d.md", time.Now().UnixNano()))
	args := []string{"exec", "--json", "--skip-git-repo-check", "--color", "never"}
	workdir := firstNonEmpty(c.input.Workdir, c.input.RepoPath)
	if err := isolation.PrepareWorkdir(runCtx, workdir); err != nil {
		return err
	}
	c.mcpPolicy = strings.TrimSpace(c.input.Env["VES_CODEX_MCP_POLICY"])
	c.mcpDiscovery = "skipped"
	if c.mcpPolicy == "minimal" {
		c.mcpDiscovery = "failed"
		servers, discovery, err := configuredCodexMCPServers(runCtx, workdir, c.input.Env)
		if err == nil {
			overrides := minimalMCPConfigOverrides(servers, c.input.Env["VES_CODEX_MCP_ALLOWLIST"])
			for _, override := range overrides {
				args = append(args, "--config", override)
			}
			c.mcpDisabled = len(overrides)
			c.mcpDiscovery = discovery
		}
	}
	if os.Getenv("VES_CODEX_EXTERNAL_SANDBOX") == "1" {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	if model := strings.TrimSpace(c.input.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := strings.TrimSpace(c.input.ReasoningEffort); effort != "" {
		args = append(args, "--config", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	args = append(args, "--output-last-message", outFile, prompt)
	cmd := isolation.CommandContext(runCtx, workdir, "codex", args...)
	cmd.Env = runnerEnvironment(c.input.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	c.cmd = cmd
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		defer close(c.events)
		c.events <- Event{Type: "agent.progress", Message: "codex started", Data: map[string]any{
			"mcp_policy":         c.mcpPolicy,
			"mcp_disabled_count": c.mcpDisabled,
			"mcp_discovery":      c.mcpDiscovery,
		}}
		parser := newCodexEventParser()
		var transcript bytes.Buffer
		var transcriptMu sync.Mutex
		var wg sync.WaitGroup
		streamPipe := func(name string, r io.Reader) {
			defer wg.Done()
			reader := bufio.NewReaderSize(r, 64*1024)
			for {
				line, readErr := reader.ReadString('\n')
				line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
				if line != "" {
					transcriptMu.Lock()
					transcript.WriteString(line)
					transcript.WriteByte('\n')
					transcriptMu.Unlock()
					if name == "stdout" {
						c.events <- parser.parse(line, task.Name)
					} else {
						c.events <- Event{Type: "agent.warning", Message: line, Data: map[string]any{"stream": name, "role": task.Name, "kind": "runtime"}, Raw: line}
					}
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					c.events <- Event{Type: "warning", Message: fmt.Sprintf("codex %s stream error: %v", name, readErr)}
					break
				}
			}
		}
		wg.Add(2)
		go streamPipe("stdout", stdout)
		go streamPipe("stderr", stderr)
		wg.Wait()
		err := cmd.Wait()
		transcriptMu.Lock()
		transcriptText := transcript.String()
		transcriptMu.Unlock()
		transcriptText = strings.TrimSpace(transcriptText)
		out := transcriptText
		if b, readErr := os.ReadFile(outFile); readErr == nil && strings.TrimSpace(string(b)) != "" {
			out = strings.TrimSpace(string(b))
		}
		_ = os.Remove(outFile)
		status := "ok"
		if err != nil {
			status = "failed"
			c.result = Result{Status: status, Output: transcriptText, Model: "codex"}
			c.events <- Event{Type: "error", Message: err.Error()}
			return
		}
		model := strings.TrimSpace(c.input.Model)
		if model == "" {
			model = "codex-default"
		}
		c.result = Result{Status: status, Output: out, Evidence: evidenceText(out), Model: model, FilesChanged: changedFiles(cmd.Dir), Meta: map[string]any{"reasoning_effort": c.input.ReasoningEffort}}
		c.events <- Event{Type: "agent.message", Message: "codex completed", Data: map[string]any{"role": task.Name}}
	}()
	return nil
}

func configuredCodexMCPServers(ctx context.Context, workdir string, env map[string]string) ([]codexMCPServer, string, error) {
	key := strings.Join([]string{os.Getenv("VES_RUNNER_USER"), os.Getenv("VES_RUNNER_HOME"), os.Getenv("CODEX_HOME")}, "\x00")
	codexMCPServerCache.Lock()
	defer codexMCPServerCache.Unlock()
	if cached, ok := codexMCPServerCache.servers[key]; ok {
		return append([]codexMCPServer(nil), cached...), "memory_cache", nil
	}
	if raw := strings.TrimSpace(env["VES_CODEX_MCP_SERVERS_JSON"]); raw != "" {
		servers, err := decodeCodexMCPServers([]byte(raw))
		if err != nil {
			return nil, "environment", err
		}
		codexMCPServerCache.servers[key] = append([]codexMCPServer(nil), servers...)
		codexMCPServerCache.sources[key] = "snapshot"
		return servers, "snapshot", nil
	}
	if cacheFile := strings.TrimSpace(env["VES_CODEX_MCP_SERVERS_FILE"]); cacheFile != "" {
		raw, err := os.ReadFile(filepath.Clean(cacheFile))
		if err == nil {
			servers, decodeErr := decodeCodexMCPServers(raw)
			if decodeErr != nil {
				return nil, "snapshot", decodeErr
			}
			codexMCPServerCache.servers[key] = append([]codexMCPServer(nil), servers...)
			codexMCPServerCache.sources[key] = "snapshot"
			return servers, "snapshot", nil
		}
	}
	cmd := isolation.CommandContext(ctx, workdir, "codex", "mcp", "list", "--json")
	cmd.Env = runnerEnvironment(env)
	output, err := cmd.Output()
	if err != nil {
		return nil, "command", fmt.Errorf("list Codex MCP servers: %w", err)
	}
	servers, err := decodeCodexMCPServers(output)
	if err != nil {
		return nil, "command", err
	}
	codexMCPServerCache.servers[key] = append([]codexMCPServer(nil), servers...)
	codexMCPServerCache.sources[key] = "command"
	return servers, "command", nil
}

func decodeCodexMCPServers(raw []byte) ([]codexMCPServer, error) {
	var servers []codexMCPServer
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("parse Codex MCP server list: %w", err)
	}
	return servers, nil
}

func minimalMCPConfigOverrides(servers []codexMCPServer, allowlist string) []string {
	allowed := map[string]bool{}
	for _, name := range strings.Split(allowlist, ",") {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = true
		}
	}
	var overrides []string
	for _, server := range servers {
		if server.Enabled && !allowed[server.Name] {
			overrides = append(overrides, "mcp_servers."+server.Name+".enabled=false")
		}
	}
	return overrides
}

func runnerEnvironment(extra map[string]string) []string {
	return isolation.ModelEnvironment(extra)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *CodexRunner) runStub(ctx context.Context, task Task, prompt string) {
	defer close(c.events)
	c.events <- Event{Type: "agent.progress", Message: "codex stub: " + task.Name}
	phase := c.input.Phase
	out := fmt.Sprintf("STUB_RUNNER phase=%s task=%s\n", phase, task.Name)
	// Write a marker file so coding phases leave a footprint in local mode
	if c.input.Workdir != "" && (phase == "code" || phase == "build" || phase == "prompt") {
		_ = os.WriteFile(c.input.Workdir+"/.vessica-runner-stub", []byte(out+prompt[:min(200, len(prompt))]), 0o644)
	}
	c.result = Result{
		Status:   "ok",
		Output:   out,
		Model:    "codex-stub",
		Meta:     map[string]any{"stub": true, "phase": phase},
		Evidence: "stub",
	}
	c.events <- Event{Type: "agent.message", Message: out}
	select {
	case <-ctx.Done():
	default:
	}
}

func (c *CodexRunner) StreamEvents(ctx context.Context) (<-chan Event, error) {
	if c.events == nil {
		return nil, fmt.Errorf("runner not prepared")
	}
	return c.events, nil
}

func (c *CodexRunner) Cancel(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

func (c *CodexRunner) CollectResult(ctx context.Context) (Result, error) {
	// Wait for events channel closed
	if c.events != nil {
		for range c.events {
		}
	}
	if c.result.Status == "" {
		c.result.Status = "ok"
	}
	return c.result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func evidenceText(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "codex completed"
	}
	return truncate(out, 2000)
}

func runnerTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("VES_CODEX_TIMEOUT"))
	if raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	return 20 * time.Minute
}

func changedFiles(workdir string) []string {
	if workdir == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", workdir, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	return parseChangedFiles(string(out))
}

func parseChangedFiles(out string) []string {
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if len(line) > 3 {
			name := strings.TrimSpace(line[3:])
			if _, renamed, ok := strings.Cut(name, " -> "); ok {
				name = renamed
			}
			files = append(files, name)
		}
	}
	return files
}

// New returns a runner by name.
func New(name string) (Runner, error) {
	switch strings.ToLower(name) {
	case "", "codex":
		return NewCodex(), nil
	case "claude", "cursor", "pi":
		if os.Getenv("VES_RUNNER_MODE") != "stub" && os.Getenv("VES_SIMULATION") != "1" {
			return nil, fmt.Errorf("%s runner is not implemented in v1; use codex or set VES_RUNNER_MODE=stub for simulation", name)
		}
		r := NewCodex()
		r.useStub = true
		return &namedRunner{Runner: r, name: name}, nil
	default:
		return nil, fmt.Errorf("unsupported runner: %s", name)
	}
}

type namedRunner struct {
	Runner
	name string
}

func (n *namedRunner) Name() string { return n.name }
