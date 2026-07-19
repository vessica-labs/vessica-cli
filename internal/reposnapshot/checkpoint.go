// Package reposnapshot defines the persisted contract for repository-bearing
// Railway sandbox checkpoints.
package reposnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/harness"
)

const SchemaVersion = 3

const MarkerFile = ".vessica-repository-checkpoint.json"
const CandidateFile = ".vessica-repository-checkpoint-candidate.json"

var dependencyFiles = []string{
	"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock",
	"go.mod", "go.sum", "pyproject.toml", "uv.lock", "poetry.lock", "requirements.txt",
	"Cargo.toml", "Cargo.lock", "Gemfile", "Gemfile.lock", "pom.xml", "build.gradle", "build.gradle.kts", "gradle.properties", "composer.json", "composer.lock",
}

var contractFiles = []string{".vessica/harness.yaml", "ARCHITECTURE.md", "TESTING.md"}

// Checkpoint describes an immutable Railway disk snapshot containing a clean
// repository checkout and its warmed dependency state. Secrets and Railway
// variables are never part of this document or the checkpoint contract.
type Checkpoint struct {
	SchemaVersion            int           `json:"schema_version"`
	Name                     string        `json:"name"`
	Status                   string        `json:"status"`
	BaseCommit               string        `json:"base_commit"`
	DependencyFingerprint    string        `json:"dependency_fingerprint"`
	ToolchainFingerprint     string        `json:"toolchain_fingerprint"`
	Stack                    string        `json:"stack"`
	DependencyState          string        `json:"dependency_state"`
	SpecificationFingerprint string        `json:"specification_fingerprint"`
	Specification            Specification `json:"specification"`
	PreparedAt               string        `json:"prepared_at"`
	VerifiedAt               string        `json:"verified_at,omitempty"`
	Verification             string        `json:"verification,omitempty"`
}

// Specification is the reviewable purpose-built snapshot contract inferred
// on first installation. It records what the snapshot warmed without storing
// credentials or environment values.
type Specification struct {
	Stack           string        `json:"stack"`
	Stacks          []string      `json:"stacks,omitempty"`
	PackageManager  string        `json:"package_manager,omitempty"`
	PackageManagers []string      `json:"package_managers,omitempty"`
	Manifests       []string      `json:"manifests,omitempty"`
	RequiredTools   []string      `json:"required_tools,omitempty"`
	WorkspaceRoots  []string      `json:"workspace_roots,omitempty"`
	Environments    []Environment `json:"environments,omitempty"`
}

// Environment is one independently warmed dependency ecosystem within a
// repository. Composite repositories carry one entry per workspace root and
// stack instead of silently selecting the first manifest encountered.
type Environment struct {
	Root           string   `json:"root"`
	Stack          string   `json:"stack"`
	PackageManager string   `json:"package_manager"`
	Manifests      []string `json:"manifests,omitempty"`
	RequiredTools  []string `json:"required_tools,omitempty"`
	InstallCommand string   `json:"install_command,omitempty"`
}

type repositoryMetadata struct {
	RepositoryCheckpoint *Checkpoint    `json:"repository_checkpoint,omitempty"`
	Extra                map[string]any `json:"-"`
}

// Name returns a bounded immutable checkpoint name. A new repository commit,
// dependency graph, or base toolchain produces a different name.
func Name(canonicalRemote, commit, dependencyFingerprint, specificationFingerprint, toolchainFingerprint string) string {
	remote := shortHash(canonicalRemote, 10)
	contract := shortHash(dependencyFingerprint+"\x00"+specificationFingerprint, 10)
	return fmt.Sprintf("vessica-repo-%s-%s-%s-%s", remote, short(commit, 10), contract, short(toolchainFingerprint, 10))
}

// Ready reports whether the snapshot can safely derive from the active worker
// contract. A toolchain change intentionally invalidates repository snapshots.
func (c Checkpoint) Ready(toolchainFingerprint string) bool {
	return c.SchemaVersion == SchemaVersion && c.Status == "ready" && strings.TrimSpace(c.Name) != "" && c.ToolchainFingerprint == toolchainFingerprint && strings.TrimSpace(c.VerifiedAt) != ""
}

// Candidate reports whether a checkpoint recipe can be promoted after the
// run that produced it completes all validation and publication phases.
func (c Checkpoint) Candidate(toolchainFingerprint string) bool {
	return c.SchemaVersion == SchemaVersion && c.Status == "candidate" && strings.TrimSpace(c.Name) != "" && c.ToolchainFingerprint == toolchainFingerprint
}

// Promote marks a candidate as validated by a successful full run.
func (c Checkpoint) Promote(verification string, now time.Time) Checkpoint {
	c.Status = "ready"
	c.Verification = strings.TrimSpace(verification)
	c.VerifiedAt = now.UTC().Format(time.RFC3339Nano)
	return c
}

// Parse extracts checkpoint state while tolerating unrelated repository
// metadata written by newer or older clients.
func Parse(raw string) (Checkpoint, bool) {
	var document map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &document) != nil {
		return Checkpoint{}, false
	}
	value := document["repository_checkpoint"]
	if len(value) == 0 {
		return Checkpoint{}, false
	}
	var checkpoint Checkpoint
	if json.Unmarshal(value, &checkpoint) != nil {
		return Checkpoint{}, false
	}
	return checkpoint, true
}

// Merge stores checkpoint state without discarding unrelated metadata.
func Merge(raw string, checkpoint Checkpoint) (string, error) {
	document := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			return "", fmt.Errorf("decode repository metadata: %w", err)
		}
	}
	document["repository_checkpoint"] = checkpoint
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("encode repository metadata: %w", err)
	}
	return string(encoded), nil
}

// DependencyFingerprint hashes the bounded, recursive dependency and harness
// contract. Source-only changes retain the warmed cache, while a nested
// workspace lockfile or validation-command change invalidates it.
func DependencyFingerprint(root string) (string, error) {
	hash := sha256.New()
	found := false
	files, err := RepositoryFiles(root)
	if err != nil {
		return "", err
	}
	for _, name := range files {
		if !isContractFile(name) {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		found = true
		_, _ = io.WriteString(hash, name+"\x00")
		_, copyErr := io.Copy(hash, file)
		_ = file.Close()
		if copyErr != nil {
			return "", copyErr
		}
	}
	if !found {
		_, _ = io.WriteString(hash, "no-dependency-manifest")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// DependencyInstallCommand returns the complete ordered install plan as one
// shell command for compatibility with the worker lifecycle.
func DependencyInstallCommand(root string) (stack, command string) {
	environments, err := DependencyInstallPlan(root)
	if err != nil || len(environments) == 0 {
		return "generic", ""
	}
	var stacks, commands []string
	seen := map[string]bool{}
	for _, environment := range environments {
		if !seen[environment.Stack] {
			stacks, seen[environment.Stack] = append(stacks, environment.Stack), true
		}
		if strings.TrimSpace(environment.InstallCommand) != "" {
			commands = append(commands, "("+environment.InstallCommand+")")
		}
	}
	return strings.Join(stacks, "+"), strings.Join(commands, " && ")
}

// DependencyInstallPlan discovers conventional nested workspaces to bounded
// depth and produces one lockfile-aware install step per ecosystem.
func DependencyInstallPlan(root string) ([]Environment, error) {
	files, err := RepositoryFiles(root)
	if err != nil {
		return nil, err
	}
	return inferEnvironments(files, root), nil
}

// InferSpecification derives a portable, secret-free snapshot recipe from the
// repository inventory collected during orientation.
func InferSpecification(files []string, stack string) (Specification, string) {
	environments := inferEnvironments(files, "")
	spec := Specification{Stack: stack, Environments: environments}
	seenStacks, seenManagers, seenTools, seenRoots := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, environment := range environments {
		if !seenStacks[environment.Stack] {
			spec.Stacks = append(spec.Stacks, environment.Stack)
			seenStacks[environment.Stack] = true
		}
		if !seenManagers[environment.PackageManager] {
			spec.PackageManagers = append(spec.PackageManagers, environment.PackageManager)
			seenManagers[environment.PackageManager] = true
		}
		if !seenRoots[environment.Root] {
			spec.WorkspaceRoots = append(spec.WorkspaceRoots, environment.Root)
			seenRoots[environment.Root] = true
		}
		spec.Manifests = append(spec.Manifests, environment.Manifests...)
		for _, tool := range environment.RequiredTools {
			if !seenTools[tool] {
				spec.RequiredTools = append(spec.RequiredTools, tool)
				seenTools[tool] = true
			}
		}
	}
	if len(spec.PackageManagers) == 1 {
		spec.PackageManager = spec.PackageManagers[0]
	}
	if len(spec.Stacks) > 0 {
		spec.Stack = strings.Join(spec.Stacks, "+")
	}
	encoded, _ := json.Marshal(spec)
	sum := sha256.Sum256(encoded)
	return spec, hex.EncodeToString(sum[:])
}

func inferEnvironments(files []string, root string) []Environment {
	present := map[string]bool{}
	roots := map[string]bool{}
	for _, file := range files {
		file = filepath.ToSlash(strings.TrimSpace(file))
		present[file] = true
		if isDependencyFile(filepath.Base(file)) {
			dir := filepath.ToSlash(filepath.Dir(file))
			if dir == "" {
				dir = "."
			}
			roots[dir] = true
		}
	}
	var orderedRoots []string
	for dir := range roots {
		orderedRoots = append(orderedRoots, dir)
	}
	sort.Strings(orderedRoots)
	var environments []Environment
	for _, dir := range orderedRoots {
		at := func(name string) bool {
			path := name
			if dir != "." {
				path = dir + "/" + name
			}
			return present[path]
		}
		prefix := func(command string) string {
			if dir == "." {
				return command
			}
			return "cd " + shellQuote(dir) + " && " + command + " && cd - >/dev/null"
		}
		manifest := func(names ...string) []string {
			var out []string
			for _, name := range names {
				if at(name) {
					if dir == "." {
						out = append(out, name)
					} else {
						out = append(out, dir+"/"+name)
					}
				}
			}
			return out
		}
		absolute := root
		if absolute != "" && dir != "." {
			absolute = filepath.Join(root, filepath.FromSlash(dir))
		}
		if at("package.json") {
			manager := "npm"
			if absolute != "" {
				manager = harness.NodePackageManager(absolute)
			} else if at("pnpm-lock.yaml") {
				manager = "pnpm"
			} else if at("yarn.lock") {
				manager = "yarn"
			}
			command, tools := "npm install --no-package-lock", []string{"node", "npm"}
			switch manager {
			case "pnpm":
				command, tools = "pnpm install --frozen-lockfile", []string{"node", "pnpm"}
			case "yarn":
				command, tools = "corepack yarn install --immutable", []string{"node", "corepack"}
			default:
				if at("package-lock.json") {
					command = "npm ci"
				}
			}
			environments = append(environments, Environment{Root: dir, Stack: "node", PackageManager: manager, Manifests: manifest("package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock"), RequiredTools: tools, InstallCommand: prefix(command)})
		}
		if at("go.mod") {
			environments = append(environments, Environment{Root: dir, Stack: "go", PackageManager: "go-modules", Manifests: manifest("go.mod", "go.sum"), RequiredTools: []string{"go"}, InstallCommand: prefix("go mod download")})
		}
		if at("requirements.txt") || at("pyproject.toml") {
			command := "test -d .venv || python3 -m venv .venv; .venv/bin/pip install -e ."
			if at("requirements.txt") {
				command = "test -d .venv || python3 -m venv .venv; .venv/bin/pip install -r requirements.txt"
			}
			environments = append(environments, Environment{Root: dir, Stack: "python", PackageManager: "python", Manifests: manifest("pyproject.toml", "uv.lock", "poetry.lock", "requirements.txt"), RequiredTools: []string{"python3", "pip"}, InstallCommand: prefix(command)})
		}
		if at("Cargo.toml") {
			environments = append(environments, Environment{Root: dir, Stack: "rust", PackageManager: "cargo", Manifests: manifest("Cargo.toml", "Cargo.lock"), RequiredTools: []string{"cargo", "rustc"}, InstallCommand: prefix("cargo fetch --locked")})
		}
		if at("Gemfile") {
			environments = append(environments, Environment{Root: dir, Stack: "ruby", PackageManager: "bundler", Manifests: manifest("Gemfile", "Gemfile.lock"), RequiredTools: []string{"ruby", "bundle"}, InstallCommand: prefix("bundle config set path vendor/bundle && bundle install")})
		}
		if at("pom.xml") {
			environments = append(environments, Environment{Root: dir, Stack: "java", PackageManager: "maven", Manifests: manifest("pom.xml"), RequiredTools: []string{"java", "mvn"}, InstallCommand: prefix("mvn -q dependency:go-offline")})
		} else if at("build.gradle") || at("build.gradle.kts") {
			environments = append(environments, Environment{Root: dir, Stack: "java", PackageManager: "gradle", Manifests: manifest("build.gradle", "build.gradle.kts", "gradle.properties"), RequiredTools: []string{"java", "gradle"}, InstallCommand: prefix("if test -x ./gradlew; then ./gradlew dependencies --no-daemon; else gradle dependencies --no-daemon; fi")})
		}
	}
	return environments
}

func isDependencyFile(name string) bool {
	for _, candidate := range dependencyFiles {
		if name == candidate {
			return true
		}
	}
	return false
}
func isContractFile(path string) bool {
	if isDependencyFile(filepath.Base(path)) {
		return true
	}
	path = filepath.ToSlash(path)
	for _, candidate := range contractFiles {
		if path == candidate {
			return true
		}
	}
	return false
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

// RepositoryFiles returns the bounded inventory used by the snapshot spec.
func RepositoryFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == ".git" || relative == "node_modules" || relative == ".venv" || relative == "vendor" {
				return filepath.SkipDir
			}
			if strings.Count(relative, "/") >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) < 1000 {
			files = append(files, relative)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func short(value string, length int) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) >= length {
		return value[:length]
	}
	return shortHash(value, length)
}

func shortHash(value string, length int) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:length]
}
