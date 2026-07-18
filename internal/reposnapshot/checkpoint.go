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
	"strings"
)

const SchemaVersion = 1

const MarkerFile = ".vessica-repository-checkpoint.json"

var dependencyFiles = []string{
	"package.json", "pnpm-lock.yaml", "package-lock.json", "yarn.lock",
	"go.mod", "go.sum", "pyproject.toml", "uv.lock", "poetry.lock", "requirements.txt",
	"Cargo.toml", "Cargo.lock", "Gemfile", "Gemfile.lock", "pom.xml", "build.gradle", "build.gradle.kts", "gradle.properties", "composer.json", "composer.lock",
}

// Checkpoint describes an immutable Railway disk snapshot containing a clean
// repository checkout and its warmed dependency state. Secrets and Railway
// variables are never part of this document or the checkpoint contract.
type Checkpoint struct {
	SchemaVersion         int    `json:"schema_version"`
	Name                  string `json:"name"`
	Status                string `json:"status"`
	BaseCommit            string `json:"base_commit"`
	DependencyFingerprint string `json:"dependency_fingerprint"`
	ToolchainFingerprint  string `json:"toolchain_fingerprint"`
	Stack                 string `json:"stack"`
	DependencyState       string `json:"dependency_state"`
	PreparedAt            string `json:"prepared_at"`
}

type repositoryMetadata struct {
	RepositoryCheckpoint *Checkpoint    `json:"repository_checkpoint,omitempty"`
	Extra                map[string]any `json:"-"`
}

// Name returns a bounded immutable checkpoint name. A new repository commit,
// dependency graph, or base toolchain produces a different name.
func Name(canonicalRemote, commit, dependencyFingerprint, toolchainFingerprint string) string {
	remote := shortHash(canonicalRemote, 10)
	return fmt.Sprintf("vessica-repo-%s-%s-%s-%s", remote, short(commit, 10), short(dependencyFingerprint, 10), short(toolchainFingerprint, 10))
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
