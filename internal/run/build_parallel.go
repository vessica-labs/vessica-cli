package run

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type buildGateResult struct {
	command  namedBuildCommand
	resolved string
	output   string
	err      error
	duration time.Duration
	skipped  bool
	deferred bool
}

func executeBuildGate(ctx context.Context, workdir string, command namedBuildCommand) buildGateResult {
	result := buildGateResult{command: command, resolved: strings.TrimSpace(harness.ResolveNodeCommand(workdir, command.cmd))}
	if result.resolved == "" || strings.Contains(result.resolved, "configure ") {
		result.skipped = true
		return result
	}
	started := time.Now()
	output, err := isolation.CommandContext(ctx, workdir, "bash", "-lc", result.resolved).CombinedOutput()
	result.duration = time.Since(started)
	result.output = redaction.Redact(string(output))
	result.err = err
	return result
}

// runParallelBuildGates overlaps independent static checks with the product
// build lane while preserving build -> test ordering. On any failure, repairs
// happen serially and the complete gate set is rerun so repaired files cannot
// invalidate an earlier successful result.
func (e *Engine) runParallelBuildGates(ctx context.Context, r *state.Run, workdir string, commands []namedBuildCommand) error {
	if len(commands) != 4 {
		return fmt.Errorf("parallel build contract requires lint, lint-arch, build, and test")
	}
	results := make([]buildGateResult, len(commands))
	var wg sync.WaitGroup
	for _, index := range []int{0, 1} {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = executeBuildGate(ctx, workdir, commands[index])
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[2] = executeBuildGate(ctx, workdir, commands[2])
		if results[2].err != nil {
			results[3] = buildGateResult{command: commands[3], resolved: strings.TrimSpace(harness.ResolveNodeCommand(workdir, commands[3].cmd)), deferred: true}
			return
		}
		results[3] = executeBuildGate(ctx, workdir, commands[3])
	}()
	wg.Wait()

	failed := false
	for _, result := range results {
		if result.deferred {
			failed = true
			continue
		}
		e.recordBuildGate(ctx, r, result, false)
		failed = failed || result.err != nil
	}
	if !failed {
		return nil
	}

	for _, result := range results {
		if result.err == nil {
			continue
		}
		if _, fixErr := e.invokeRunner(ctx, r, "build", "Fix build failure: "+result.command.name+"\n"+result.output, "build", workdir); fixErr != nil && !simulationMode() {
			return fixErr
		}
	}

	for _, command := range commands {
		result := executeBuildGate(ctx, workdir, command)
		e.recordBuildGate(ctx, r, result, true)
		if result.err == nil || result.skipped {
			continue
		}
		if simulationMode() {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": command.name + " failed in simulation; continuing"})
			continue
		}
		if command.name == "lint" || command.name == "lint-arch" {
			e.emit(ctx, r.ID, "warning", map[string]any{"message": command.name + " failed; continuing"})
			continue
		}
		return fmt.Errorf("%s failed: %w", command.name, result.err)
	}
	return nil
}

func (e *Engine) recordBuildGate(ctx context.Context, r *state.Run, result buildGateResult, retried bool) {
	detail := map[string]any{"command": result.resolved, "duration_ms": result.duration.Milliseconds(), "parallel": !retried}
	if retried {
		detail["retried"] = true
	}
	if result.skipped {
		_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", result.command.name, "", "skipped", detail)
		e.emit(ctx, r.ID, "build.output", map[string]any{"step": result.command.name, "status": "skipped"})
		return
	}
	detail["output"] = truncate(result.output, 4000)
	status := "passed"
	if result.err != nil {
		status = "failed"
		detail["error"] = result.err.Error()
	}
	_, _ = e.DB.CreateRunEvidence(ctx, r.ID, "build", result.command.name, "", status, detail)
	step := result.command.name
	if retried {
		step += ":retry"
	}
	e.emit(ctx, r.ID, "build.output", map[string]any{"step": step, "command": result.resolved, "output": truncate(result.output, 4000), "status": status, "duration_ms": result.duration.Milliseconds(), "parallel": !retried})
}
