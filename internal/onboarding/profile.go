package onboarding

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/harness"
)

type RepositoryProfile struct {
	Name            string            `json:"name"`
	Root            string            `json:"root"`
	Remote          string            `json:"remote"`
	Commit          string            `json:"commit,omitempty"`
	MappedCommit    string            `json:"mapped_commit,omitempty"`
	MappedFiles     []string          `json:"mapped_files,omitempty"`
	DefaultBranch   string            `json:"default_branch,omitempty"`
	Dirty           bool              `json:"dirty"`
	Stack           string            `json:"stack"`
	Languages       []string          `json:"languages"`
	Frameworks      []string          `json:"frameworks"`
	PackageManagers []string          `json:"package_managers"`
	Manifests       []string          `json:"manifests"`
	Dependencies    []string          `json:"dependencies"`
	Components      []string          `json:"components"`
	EntryPoints     []string          `json:"entry_points"`
	Directories     []string          `json:"important_directories"`
	CI              []string          `json:"ci"`
	Deploy          []string          `json:"deploy"`
	Commands        map[string]string `json:"commands"`
	Risks           []string          `json:"risks"`
	Unresolved      []string          `json:"unresolved_questions"`
	Harness         string            `json:"harness"`
	Fingerprint     map[string]string `json:"fingerprint"`
}

func Scan(root, remote string) RepositoryProfile {
	detected := harness.Detect(root)
	p := RepositoryProfile{Name: filepath.Base(root), Root: root, Remote: remote, Stack: detected.Stack, Commands: map[string]string{"preview": detected.PreviewCommand, "build": detected.BuildCommand, "test": detected.TestCommand, "lint": detected.LintCommand}, Fingerprint: map[string]string{}}
	p.Commit = git(root, "rev-parse", "HEAD")
	p.DefaultBranch = strings.TrimPrefix(git(root, "symbolic-ref", "refs/remotes/origin/HEAD"), "refs/remotes/origin/")
	p.Dirty = git(root, "status", "--porcelain") != ""
	markers := []struct{ path, language string }{{"go.mod", "Go"}, {"package.json", "JavaScript/TypeScript"}, {"pnpm-workspace.yaml", "JavaScript/TypeScript"}, {"pyproject.toml", "Python"}, {"requirements.txt", "Python"}, {"Cargo.toml", "Rust"}, {"Gemfile", "Ruby"}}
	seen := map[string]bool{}
	for _, marker := range markers {
		if exists(filepath.Join(root, marker.path)) {
			p.Manifests = append(p.Manifests, marker.path)
			if !seen[marker.language] {
				p.Languages = append(p.Languages, marker.language)
				seen[marker.language] = true
			}
		}
	}
	p.PackageManagers = detectPackageManagers(root)
	p.Dependencies, p.Frameworks = detectDependencies(root)
	p.Components, p.Directories = detectRepositoryStructure(root)
	p.EntryPoints = detectEntryPoints(root)
	for _, candidate := range []string{".github/workflows", ".gitlab-ci.yml"} {
		if exists(filepath.Join(root, candidate)) {
			p.CI = append(p.CI, candidate)
		}
	}
	for _, candidate := range []string{"railway.json", "railway.toml", "Dockerfile", "docker-compose.yml", "fly.toml", "vercel.json"} {
		if exists(filepath.Join(root, candidate)) {
			p.Deploy = append(p.Deploy, candidate)
		}
	}
	harnessYAML, packLock := exists(filepath.Join(root, ".vessica", "harness.yaml")), exists(filepath.Join(root, ".vessica", "pack.lock"))
	switch {
	case harnessYAML && packLock:
		p.Harness = "present"
	case harnessYAML || packLock:
		p.Harness = "partial"
	default:
		p.Harness = "absent"
	}
	for _, target := range append([]string{"AGENTS.md", "ARCHITECTURE.md", "DESIGN.md", "DEPLOY.md", "TESTING.md", "SECURITY.md"}, ".vessica/harness.yaml", ".vessica/pack.lock") {
		p.Fingerprint[target] = fileHash(filepath.Join(root, target))
	}
	if p.Dirty {
		p.Risks = append(p.Risks, "Local working tree differs from the mapped remote commit")
	}
	if len(p.CI) == 0 {
		p.Risks = append(p.Risks, "No supported CI workflow marker was detected")
	}
	if len(p.Deploy) == 0 {
		p.Unresolved = append(p.Unresolved, "Deployment target and release process were not evidenced by a supported configuration file")
	}
	for _, command := range []string{"preview", "build", "test", "lint"} {
		if strings.TrimSpace(p.Commands[command]) == "" {
			p.Unresolved = append(p.Unresolved, "No manifest-backed "+command+" command was identified")
		}
	}
	if p.Languages == nil {
		p.Languages = []string{}
	}
	if p.Frameworks == nil {
		p.Frameworks = []string{}
	}
	if p.PackageManagers == nil {
		p.PackageManagers = []string{}
	}
	if p.Manifests == nil {
		p.Manifests = []string{}
	}
	if p.Dependencies == nil {
		p.Dependencies = []string{}
	}
	if p.Components == nil {
		p.Components = []string{}
	}
	if p.EntryPoints == nil {
		p.EntryPoints = []string{}
	}
	if p.Directories == nil {
		p.Directories = []string{}
	}
	if p.CI == nil {
		p.CI = []string{}
	}
	if p.Deploy == nil {
		p.Deploy = []string{}
	}
	if p.Risks == nil {
		p.Risks = []string{}
	}
	if p.Unresolved == nil {
		p.Unresolved = []string{}
	}
	sort.Strings(p.Languages)
	sort.Strings(p.Frameworks)
	sort.Strings(p.PackageManagers)
	sort.Strings(p.Manifests)
	sort.Strings(p.Dependencies)
	sort.Strings(p.Components)
	sort.Strings(p.EntryPoints)
	sort.Strings(p.Directories)
	sort.Strings(p.CI)
	sort.Strings(p.Deploy)
	return p
}

func detectPackageManagers(root string) []string {
	var out []string
	for _, item := range []struct{ file, name string }{{"pnpm-lock.yaml", "pnpm"}, {"package-lock.json", "npm"}, {"yarn.lock", "yarn"}, {"go.mod", "Go modules"}, {"uv.lock", "uv"}, {"poetry.lock", "Poetry"}, {"Cargo.lock", "Cargo"}, {"Gemfile.lock", "Bundler"}} {
		if exists(filepath.Join(root, item.file)) {
			out = append(out, item.name)
		}
	}
	return out
}

func detectDependencies(root string) ([]string, []string) {
	dependencies := map[string]bool{}
	if body, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		var manifest struct {
			Dependencies    map[string]json.RawMessage `json:"dependencies"`
			DevDependencies map[string]json.RawMessage `json:"devDependencies"`
		}
		if json.Unmarshal(body, &manifest) == nil {
			for name := range manifest.Dependencies {
				dependencies[name] = true
			}
			for name := range manifest.DevDependencies {
				dependencies[name] = true
			}
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		for _, line := range strings.Split(string(body), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && (strings.Contains(fields[0], ".") || strings.Contains(fields[0], "/")) && strings.HasPrefix(fields[1], "v") {
				dependencies[fields[0]] = true
			}
		}
	}
	var out, frameworks []string
	frameworkNames := map[string]string{"next": "Next.js", "react": "React", "vite": "Vite", "vue": "Vue", "svelte": "Svelte", "express": "Express", "github.com/gin-gonic/gin": "Gin", "github.com/labstack/echo": "Echo", "github.com/gofiber/fiber": "Fiber"}
	for name := range dependencies {
		out = append(out, name)
		if framework := frameworkNames[name]; framework != "" {
			frameworks = append(frameworks, framework)
		}
	}
	if len(out) > 40 {
		sort.Strings(out)
		out = out[:40]
	}
	return out, frameworks
}

func detectRepositoryStructure(root string) ([]string, []string) {
	var components, directories []string
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor" {
			continue
		}
		directories = append(directories, entry.Name()+"/")
		if entry.Name() == "apps" || entry.Name() == "packages" || entry.Name() == "services" || entry.Name() == "cmd" {
			children, _ := os.ReadDir(filepath.Join(root, entry.Name()))
			for _, child := range children {
				if child.IsDir() && !strings.HasPrefix(child.Name(), ".") {
					components = append(components, filepath.ToSlash(filepath.Join(entry.Name(), child.Name()))+"/")
				}
			}
		} else if entry.Name() == "src" || entry.Name() == "internal" || entry.Name() == "web" || entry.Name() == "api" || entry.Name() == "server" || entry.Name() == "client" {
			components = append(components, entry.Name()+"/")
		}
	}
	return components, directories
}

func detectEntryPoints(root string) []string {
	patterns := []string{"main.go", "cmd/*/main.go", "src/main.*", "src/index.*", "app.*", "server.*"}
	var out []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, match := range matches {
			if stat, err := os.Stat(match); err == nil && !stat.IsDir() {
				relative, _ := filepath.Rel(root, match)
				out = append(out, filepath.ToSlash(relative))
			}
		}
	}
	return out
}

func git(root string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
func exists(path string) bool { _, err := os.Stat(path); return err == nil }
func fileHash(path string) string {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "absent"
	}
	if err != nil {
		return "unreadable"
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
