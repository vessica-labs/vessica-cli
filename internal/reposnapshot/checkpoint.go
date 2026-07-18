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
)

const SchemaVersion = 2

const MarkerFile = ".vessica-repository-checkpoint.json"
const CandidateFile = ".vessica-repository-checkpoint-candidate.json"

var dependencyFiles = []string{
	"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock",
	"go.mod", "go.sum", "pyproject.toml", "uv.lock", "poetry.lock", "requirements.txt",
	"Cargo.toml", "Cargo.lock", "Gemfile", "Gemfile.lock", "pom.xml", "build.gradle", "build.gradle.kts", "gradle.properties", "composer.json", "composer.lock",
}

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
}

// Specification is the reviewable purpose-built snapshot contract inferred
// on first installation. It records what the snapshot warmed without storing
// credentials or environment values.
type Specification struct {
	Stack          string   `json:"stack"`
	PackageManager string   `json:"package_manager,omitempty"`
	Manifests      []string `json:"manifests,omitempty"`
	RequiredTools  []string `json:"required_tools,omitempty"`
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
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
	return c.SchemaVersion == SchemaVersion && c.Status == "ready" && strings.TrimSpace(c.Name) != "" && c.ToolchainFingerprint == toolchainFingerprint
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

// DependencyFingerprint hashes only files that affect dependency resolution.
// Source-only changes therefore retain the warmed dependency cache.
func DependencyFingerprint(root string) (string, error) {
	hash := sha256.New()
	found := false
	for _, name := range dependencyFiles {
		path := filepath.Join(root, name)
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
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

// DependencyInstallCommand returns a lockfile-aware cache refresh command for
// the common repository stacks supported by purpose-built checkpoints.
func DependencyInstallCommand(root string) (stack, command string) {
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(root, name)); return err == nil }
	switch {
	case exists("package.json") && exists("pnpm-lock.yaml"):
		return "node", "pnpm install --frozen-lockfile"
	case exists("package.json") && exists("package-lock.json"):
		return "node", "npm ci"
	case exists("package.json") && exists("yarn.lock"):
		return "node", "corepack yarn install --immutable"
	case exists("package.json"):
		return "node", "pnpm install --no-lockfile"
	case exists("go.mod"):
		return "go", "go mod download"
	case exists("requirements.txt"):
		return "python", "test -d .venv || python3 -m venv .venv; .venv/bin/pip install -r requirements.txt"
	case exists("pyproject.toml"):
		return "python", "test -d .venv || python3 -m venv .venv; .venv/bin/pip install -e ."
	case exists("Cargo.toml"):
		return "rust", "cargo fetch --locked"
	case exists("Gemfile"):
		return "ruby", "bundle config set path vendor/bundle && bundle install"
	case exists("build.gradle"), exists("build.gradle.kts"):
		return "java", "if test -x ./gradlew; then ./gradlew dependencies --no-daemon; else gradle dependencies --no-daemon; fi"
	case exists("pom.xml"):
		return "java", "mvn -q dependency:go-offline"
	default:
		return "generic", ""
	}
}

// InferSpecification derives a portable, secret-free snapshot recipe from the
// repository inventory collected during orientation.
func InferSpecification(files []string, stack string) (Specification, string) {
	present := map[string]bool{}
	for _, file := range files {
		present[filepath.ToSlash(strings.TrimSpace(file))] = true
	}
	spec := Specification{Stack: stack, WorkspaceRoots: []string{"."}}
	for _, name := range dependencyFiles {
		if present[name] {
			spec.Manifests = append(spec.Manifests, name)
		}
	}
	switch {
	case present["pnpm-lock.yaml"]:
		spec.PackageManager, spec.RequiredTools = "pnpm", []string{"node", "pnpm"}
	case present["package-lock.json"]:
		spec.PackageManager, spec.RequiredTools = "npm", []string{"node", "npm"}
	case present["yarn.lock"]:
		spec.PackageManager, spec.RequiredTools = "yarn", []string{"node", "corepack"}
	case present["go.mod"]:
		spec.PackageManager, spec.RequiredTools = "go-modules", []string{"go"}
	case present["requirements.txt"] || present["pyproject.toml"]:
		spec.PackageManager, spec.RequiredTools = "python", []string{"python3", "pip"}
	case present["Cargo.toml"]:
		spec.PackageManager, spec.RequiredTools = "cargo", []string{"cargo", "rustc"}
	case present["Gemfile"]:
		spec.PackageManager, spec.RequiredTools = "bundler", []string{"ruby", "bundle"}
	case present["pom.xml"]:
		spec.PackageManager, spec.RequiredTools = "maven", []string{"java", "mvn"}
	case present["build.gradle"] || present["build.gradle.kts"]:
		spec.PackageManager, spec.RequiredTools = "gradle", []string{"java", "gradle"}
	}
	encoded, _ := json.Marshal(spec)
	sum := sha256.Sum256(encoded)
	return spec, hex.EncodeToString(sum[:])
}

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
