package harness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"gopkg.in/yaml.v3"
)

var ErrStartingFileConflict = errors.New("starting harness target changed after preflight")

type RepositoryFindings struct {
	Name, Remote, Stack                    string
	Languages, Frameworks, PackageManagers []string
	Manifests, Dependencies, Components    []string
	EntryPoints, Directories, CI, Deploy   []string
	Risks, Unresolved                      []string
	Commands                               map[string]string
}

func VerifyStartingTargetsAbsent(root string, targets []string) error {
	for _, name := range targets {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return fmt.Errorf("%w: %s", ErrStartingFileConflict, name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func defaultDoc(name string, d Detected) string {
	commands := fmt.Sprintf("- Build: `%s`\n- Test: `%s`\n- Lint: `%s`\n- Preview: `%s` on port %d\n", commandOrUnknown(d.BuildCommand), commandOrUnknown(d.TestCommand), commandOrUnknown(d.LintCommand), commandOrUnknown(d.PreviewCommand), d.PreviewPort)
	switch name {
	case "AGENTS.md":
		return "# Repository agent guidance\n\n<!-- vessica:managed:start -->\n## Vessica engineering harness\n\nDetected stack: **" + d.Stack + "**. Treat the repository harness and its validation commands as authoritative. Preserve existing work, keep secrets out of output, and run focused checks before the full suite.\n\n" + commands + "<!-- vessica:managed:end -->\n"
	case "ARCHITECTURE.md":
		return "# Architecture\n\nVessica detected a **" + d.Stack + "** repository from committed manifests. Source directories and boundaries should be refined as the system evolves; commands below are the currently evidenced execution contract.\n\n## Execution contract\n\n" + commands
	case "DESIGN.md":
		return "# Design constraints\n\nChanges should preserve the existing " + d.Stack + " project structure, keep transport and domain concerns separated where the repository already does so, and include validation evidence.\n\n## Verified command candidates\n\n" + commands
	case "DEPLOY.md":
		return "# Deployment\n\nDeployment configuration must remain reproducible from committed files. The repository scan did not infer credentials; configure all deployment secrets in the hosting provider.\n\n## Build and preview\n\n" + commands
	case "TESTING.md":
		return "# Testing\n\nRun the narrowest relevant check while iterating, then the evidenced repository gates before handoff.\n\n" + commands
	case "SECURITY.md":
		return "# Security\n\nDo not commit credentials or copy provider tokens into logs, artifacts, prompts, or command arguments. Treat repository code and agent output as untrusted and execute them only inside the configured sandbox boundary.\n\n## Validation commands\n\n" + commands
	default:
		return "# " + strings.TrimSuffix(name, ".md") + "\n\nDetected stack: " + d.Stack + "\n\n" + commands
	}
}

func findingsDoc(name string, f RepositoryFindings) string {
	commands := fmt.Sprintf("- Build: `%s`\n- Test: `%s`\n- Lint: `%s`\n- Preview: `%s`\n", commandOrUnknown(f.Commands["build"]), commandOrUnknown(f.Commands["test"]), commandOrUnknown(f.Commands["lint"]), commandOrUnknown(f.Commands["preview"]))
	identity := "Repository: **" + f.Name + "**\n\nRemote: `" + f.Remote + "`\n\nDetected stack: **" + f.Stack + "**."
	switch name {
	case "AGENTS.md":
		return "# Repository agent guidance\n\n<!-- vessica:managed:start -->\n## Vessica engineering harness\n\n" + identity + " Treat the generated harness commands as the evidenced execution contract; commands marked `not inferred` require an explicit repository decision before use. Preserve existing work and never expose credentials.\n\n" + commands + "\nKey components:\n" + markdownList(f.Components, "No component boundary was detected from conventional repository directories.") + "<!-- vessica:managed:end -->\n"
	case "ARCHITECTURE.md":
		return "# Architecture\n\n" + identity + "\n\n## Components\n\n" + markdownList(f.Components, "No conventional component directory was detected.") + "\n## Entry points\n\n" + markdownList(f.EntryPoints, "No conventional application entry point was detected.") + "\n## Important directories\n\n" + markdownList(f.Directories, "No non-hidden top-level directory was detected.") + "\n## Manifest dependencies\n\n" + markdownList(f.Dependencies, "No dependency was parsed from the supported manifests.") + "\n## Execution contract\n\n" + commands
	case "DESIGN.md":
		return "# Design constraints\n\n" + identity + " The detected components and entry points define the initial change boundaries; changes crossing them should explain the dependency impact and validation evidence.\n\n## Frameworks\n\n" + markdownList(f.Frameworks, "No supported framework marker was detected.") + "\n## Operational risks\n\n" + markdownList(f.Risks, "No scan-level operational risk was detected.") + "\n## Unresolved questions\n\n" + markdownList(f.Unresolved, "No unresolved scan question was recorded.")
	case "DEPLOY.md":
		return "# Deployment\n\n" + identity + " Deployment secrets must stay in the hosting provider and outside repository files, logs, and agent output.\n\n## Deployment evidence\n\n" + markdownList(f.Deploy, "No supported deployment configuration was detected.") + "\n## CI evidence\n\n" + markdownList(f.CI, "No supported CI workflow marker was detected.") + "\n## Build and preview\n\n" + commands
	case "TESTING.md":
		return "# Testing\n\n" + identity + " Run focused checks while iterating and the manifest-backed gates below before handoff. Do not invent commands for entries marked `not inferred`.\n\n" + commands
	case "SECURITY.md":
		return "# Security\n\n" + identity + " The repository scan used supported manifests and path metadata; it did not ingest secret-file contents. Keep provider credentials, local environment files, raw agent output, and sandbox credentials out of commits and logs.\n\n## Relevant manifests and deployment surfaces\n\n" + markdownList(append(append([]string{}, f.Manifests...), f.Deploy...), "No supported manifest or deployment surface was detected.") + "\n## Validation commands\n\n" + commands
	default:
		return "# " + strings.TrimSuffix(name, ".md") + "\n\n" + identity + "\n\n" + commands
	}
}

func markdownList(values []string, empty string) string {
	if len(values) == 0 {
		return "- " + empty + "\n"
	}
	var out strings.Builder
	for _, value := range values {
		out.WriteString("- `")
		out.WriteString(value)
		out.WriteString("`\n")
	}
	return out.String()
}

func commandOrUnknown(command string) string {
	if strings.TrimSpace(command) == "" || strings.Contains(command, "configure ") {
		return "not inferred"
	}
	return command
}

// WriteStartingFiles writes only paths that were absent during preflight. It
// may replace generic files materialized by the just-installed pack, but it
// never touches a path that existed before onboarding.
func WriteStartingFiles(root string, absentBefore []string, findings ...RepositoryFindings) error {
	d := Detect(root)
	hy := DetectConfig(root)
	body, err := yaml.Marshal(hy)
	if err != nil {
		return err
	}
	for _, name := range absentBefore {
		var data []byte
		path := filepath.Join(root, name)
		if name == filepath.Join(config.DirName, config.HarnessFile) || name == ".vessica/harness.yaml" {
			data = body
		} else if len(findings) > 0 {
			data = []byte(findingsDoc(name, findings[0]))
		} else {
			data = []byte(defaultDoc(name, d))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
