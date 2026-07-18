package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/isolation"
	"github.com/vessica-labs/vessica-cli/internal/receipt"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

const maxContextPacketBytes = 6000

var inlineVesCommandPattern = regexp.MustCompile("`(ves\\s+[^`]+)`")

type contextPacketMeta struct {
	Artifacts int
	Contracts int
	Bytes     int
	Duration  time.Duration
}

func (e *Engine) coderContextPacket(ctx context.Context, r *state.Run, workdir, ticketText string) (string, contextPacketMeta) {
	started := time.Now()
	sections := []string{focusedValidationGuidance(workdir, ticketText)}
	meta := contextPacketMeta{}

	if artifacts, err := e.DB.ListArtifacts(ctx, r.EpicID, ""); err == nil {
		var summaries []string
		for _, artifact := range artifactsForRun(artifacts, r.ID) {
			if len(summaries) == 4 {
				break
			}
			summaries = append(summaries, fmt.Sprintf("%s: %s\n%s", artifact.Type, artifact.Title, truncate(strings.TrimSpace(artifact.Body), 700)))
		}
		if len(summaries) > 0 {
			sections = append(sections, "Authoritative planning artifacts for this run:\n"+strings.Join(summaries, "\n\n"))
			meta.Artifacts = len(summaries)
		}
	}

	if commands := commandHelpContracts(ctx, ticketText); len(commands) > 0 {
		sections = append(sections, "Version-matched Vessica CLI help:\n"+strings.Join(commands, "\n\n"))
		meta.Contracts += len(commands)
	}
	lower := strings.ToLower(ticketText)
	if strings.Contains(lower, "run receipt") || strings.Contains(lower, "wall_elapsed") || strings.Contains(lower, "infrastructure") {
		sections = append(sections, "Version-matched receipt contract:\n"+receipt.ContextContract())
		meta.Contracts++
	}

	packet := boundContextPacket(strings.TrimSpace(strings.Join(sections, "\n\n---\n\n")))
	meta.Bytes = len(packet)
	meta.Duration = time.Since(started)
	return packet, meta
}

func boundContextPacket(packet string) string {
	if len(packet) > maxContextPacketBytes {
		return packet[:maxContextPacketBytes-3] + "..."
	}
	return packet
}

func focusedValidationGuidance(workdir, ticketText string) string {
	ticketType := "code"
	lower := strings.ToLower(ticketText)
	for _, marker := range []string{"documentation", "document ", "docs", "readme", "guide", "reference"} {
		if strings.Contains(lower, marker) {
			ticketType = "documentation"
			break
		}
	}
	lines := []string{
		"Focused validation contract:",
		"- Always run: git diff --check",
		"- Do not run repository-wide build, lint, test, or preview commands; the engine owns those gates.",
		"- Do not invent one-off Node, Python, or shell assertion programs when no repository-provided focused check exists.",
	}
	if ticketType == "documentation" {
		lines = append(lines, "- For this documentation ticket, inspect the changed section directly and stop after git diff --check unless the repository already provides a named docs-only check.")
	} else if detected := harness.Detect(workdir).Stack; detected != "" {
		lines = append(lines, "- For this "+detected+" repository, use only an existing package/file-scoped test command relevant to the changed code.")
	}
	return strings.Join(lines, "\n")
}

func commandHelpContracts(ctx context.Context, text string) []string {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var contracts []string
	for _, match := range inlineVesCommandPattern.FindAllStringSubmatch(text, -1) {
		fields := strings.Fields(match[1])
		if len(fields) < 2 || fields[0] != "ves" {
			continue
		}
		var path []string
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") || strings.HasPrefix(field, "<") || strings.ContainsAny(field, "|;&") {
				break
			}
			path = append(path, field)
			if len(path) == 3 {
				break
			}
		}
		key := strings.Join(path, " ")
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		args := append(append([]string{}, path...), "--help")
		commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		cmd := exec.CommandContext(commandCtx, executable, args...)
		cmd.Dir = filepath.Clean(firstNonEmptyString(os.TempDir(), "/tmp"))
		cmd.Env = isolation.Environment(nil)
		output, commandErr := cmd.CombinedOutput()
		cancel()
		if commandErr != nil || len(output) == 0 {
			continue
		}
		contracts = append(contracts, "$ ves "+key+" --help\n"+truncate(strings.TrimSpace(string(output)), 1400))
	}
	return contracts
}
