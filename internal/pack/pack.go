package pack

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

//go:embed all:software-harness
var embeddedPack embed.FS

const (
	DefaultRef     = "@vessica/engineering-harness"
	LegacyRef      = "@vessica/software-harness"
	DefaultOrigin  = "https://github.com/vessica-labs/engineering-harness.git"
	DefaultVersion = "v1.0.0"
)

var managedEntries = []string{
	"agents",
	"docs",
	"templates",
	"workflows",
	"harness.yaml",
	"lint-arch.sh",
	"pack.yaml",
}

type Lockfile struct {
	Ref         string `json:"ref"`
	Origin      string `json:"origin"`
	Version     string `json:"version"`
	CommitSHA   string `json:"commit_sha"`
	InstalledAt string `json:"installed_at"`
	Pinned      bool   `json:"pinned"`
}

// Install resolves a built-in alias or Git source, copies it into .vessica,
// and records the exact commit used.
func Install(root, source string) (*Lockfile, error) {
	origin, version, ref, isDefault, err := parseSource(source)
	if err != nil {
		return nil, err
	}
	lock, err := installGit(root, origin, version, ref)
	if err == nil {
		return lock, nil
	}
	if isDefault {
		return installEmbedded(root)
	}
	return nil, err
}

// EnsureHosted reuses a complete committed pack and falls back directly to the
// embedded release snapshot. Hosted workers must not clone the same immutable
// engineering pack on every run.
func EnsureHosted(root string) (*Lockfile, error) {
	if lock, err := ReadLock(root); err == nil {
		complete := true
		for _, entry := range []string{"harness.yaml", "pack.yaml", "agents"} {
			if _, statErr := os.Stat(filepath.Join(root, config.DirName, entry)); statErr != nil {
				complete = false
				break
			}
		}
		if complete {
			return lock, nil
		}
	}
	return installEmbedded(root)
}

func installGit(root, origin, version, ref string) (*Lockfile, error) {
	tmp, err := os.MkdirTemp("", "vessica-pack-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	checkout := filepath.Join(tmp, "checkout")
	if err := runGit("clone", "--quiet", "--no-checkout", "--", origin, checkout); err != nil {
		return nil, fmt.Errorf("clone pack %q: %w", origin, err)
	}
	if err := runGit("-C", checkout, "checkout", "--quiet", "--detach", version); err != nil {
		return nil, fmt.Errorf("resolve pack ref %q from %q: %w", version, origin, err)
	}
	sha, err := gitOutput("-C", checkout, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve pack commit: %w", err)
	}
	if err := validatePack(checkout); err != nil {
		return nil, err
	}
	if err := installFromDir(root, checkout); err != nil {
		return nil, err
	}

	lock := &Lockfile{
		Ref:         ref,
		Origin:      origin,
		Version:     version,
		CommitSHA:   sha,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Pinned:      true,
	}
	if err := WriteLock(root, lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func installEmbedded(root string) (*Lockfile, error) {
	ves := config.Dir(root)
	if err := clearManagedEntries(ves); err != nil {
		return nil, err
	}
	if err := copyFS(embeddedPack, "software-harness", ves); err != nil {
		return nil, err
	}
	if err := materializeDocs(root, func(name string) ([]byte, error) {
		return embeddedPack.ReadFile(path.Join("software-harness", "docs", name))
	}); err != nil {
		return nil, err
	}

	lock := &Lockfile{
		Ref:         DefaultRef,
		Origin:      DefaultOrigin,
		Version:     DefaultVersion,
		CommitSHA:   "embedded-" + strings.TrimPrefix(DefaultVersion, "v"),
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Pinned:      true,
	}
	if err := WriteLock(root, lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func installFromDir(root, src string) error {
	ves := config.Dir(root)
	if err := clearManagedEntries(ves); err != nil {
		return err
	}
	if err := copyDir(src, ves); err != nil {
		return err
	}
	return materializeDocs(root, func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(src, "docs", name))
	})
}

func validatePack(root string) error {
	for _, required := range []string{"pack.yaml", "harness.yaml", "agents"} {
		if _, err := os.Stat(filepath.Join(root, required)); err != nil {
			return fmt.Errorf("invalid engineering harness: missing %s", required)
		}
	}
	return nil
}

func clearManagedEntries(ves string) error {
	if err := os.MkdirAll(ves, 0o755); err != nil {
		return err
	}
	for _, entry := range managedEntries {
		if err := os.RemoveAll(filepath.Join(ves, entry)); err != nil {
			return err
		}
	}
	return nil
}

func copyFS(src fs.FS, srcRoot, destRoot string) error {
	return fs.WalkDir(src, srcRoot, func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, name)
		if err != nil || rel == "." {
			return err
		}
		dest := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := fs.ReadFile(src, name)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if filepath.Base(name) == "lint-arch.sh" {
			mode = 0o755
		}
		return os.WriteFile(dest, data, mode)
	})
}

func copyDir(srcRoot, destRoot string) error {
	return filepath.WalkDir(srcRoot, func(name string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, name)
		if err != nil || rel == "." {
			return err
		}
		topLevel := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if !isManagedEntry(topLevel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid engineering harness: symlinks are not supported (%s)", rel)
		}
		dest := filepath.Join(destRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return os.WriteFile(dest, data, mode)
	})
}

func isManagedEntry(name string) bool {
	for _, entry := range managedEntries {
		if name == entry {
			return true
		}
	}
	return false
}

func materializeDocs(root string, read func(string) ([]byte, error)) error {
	for _, name := range []string{"AGENTS.md", "ARCHITECTURE.md", "DESIGN.md", "DEPLOY.md", "TESTING.md", "SECURITY.md"} {
		dest := filepath.Join(root, name)
		if fileExists(dest) {
			continue
		}
		data, err := read(name)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func WriteLock(root string, lock *Lockfile) error {
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, config.DirName, config.PackLockFile), append(data, '\n'), 0o644)
}

func ReadLock(root string) (*Lockfile, error) {
	data, err := os.ReadFile(filepath.Join(root, config.DirName, config.PackLockFile))
	if err != nil {
		return nil, err
	}
	var lock Lockfile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func Pin(root, version string) (*Lockfile, error) {
	lock, err := ReadLock(root)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(lock.CommitSHA, "embedded-") {
		lock.Origin = DefaultOrigin
	}
	return installGit(root, lock.Origin, version, lock.Ref)
}

func OriginGet(root string) (string, error) {
	lock, err := ReadLock(root)
	if err != nil {
		return "", err
	}
	return lock.Origin, nil
}

func OriginSet(root, source string) error {
	origin, version, ref, _, err := parseSource(source)
	if err != nil {
		return err
	}
	lock, err := ReadLock(root)
	if err != nil {
		lock = &Lockfile{Pinned: true}
	}
	lock.Ref = ref
	lock.Origin = origin
	lock.Version = version
	lock.CommitSHA = ""
	lock.InstalledAt = ""
	return WriteLock(root, lock)
}

func AgentPrompt(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return "", fmt.Errorf("agent role is required")
	}
	data, err := embeddedPack.ReadFile(path.Join("software-harness", "agents", role, "AGENTS.md"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func Sync(root string) (*Lockfile, error) {
	lock, err := ReadLock(root)
	if err != nil {
		return Install(root, DefaultRef)
	}
	if strings.HasPrefix(lock.CommitSHA, "embedded-") {
		return Install(root, DefaultRef)
	}
	if lock.Origin == "" || lock.CommitSHA == "" {
		return nil, fmt.Errorf("pack lock is not resolved; run ves pack update")
	}
	synced, err := installGit(root, lock.Origin, lock.CommitSHA, lock.Ref)
	if err != nil {
		return nil, err
	}
	synced.Version = lock.Version
	if err := WriteLock(root, synced); err != nil {
		return nil, err
	}
	return synced, nil
}

func Update(root string) (*Lockfile, error) {
	lock, err := ReadLock(root)
	if err != nil {
		return Install(root, DefaultRef)
	}
	if lock.Origin == "" {
		lock.Origin = DefaultOrigin
	}
	if lock.Version == "" {
		lock.Version = DefaultVersion
	}
	if lock.Ref == "" {
		lock.Ref = DefaultRef
	}
	return installGit(root, lock.Origin, lock.Version, lock.Ref)
}

func parseSource(source string) (origin, version, ref string, isDefault bool, err error) {
	source = strings.TrimSpace(source)
	if source == "" || source == DefaultRef || source == LegacyRef || source == "engineering-harness" || source == "software-harness" {
		return DefaultOrigin, DefaultVersion, DefaultRef, true, nil
	}
	origin = source
	version = "HEAD"
	if i := strings.LastIndex(source, "#"); i >= 0 {
		origin = strings.TrimSpace(source[:i])
		version = strings.TrimSpace(source[i+1:])
	}
	if origin == "" || version == "" {
		return "", "", "", false, fmt.Errorf("invalid pack source %q; use <git-url>[#ref]", source)
	}
	return origin, version, origin, false, nil
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("%s", message)
		}
		return err
	}
	return nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func fileExists(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}
