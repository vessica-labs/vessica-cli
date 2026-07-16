package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Tool is one executable capability required by a Vessica coding agent.
type Tool struct {
	Name       string   `json:"name"`
	Executable string   `json:"executable"`
	Args       []string `json:"-"`
	Version    string   `json:"required_version,omitempty"`
}

// Check is the machine-readable result of verifying one tool.
type Check struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	Error      string `json:"error,omitempty"`
}

var requiredTools = []Tool{
	{Name: "bash", Executable: "bash", Args: []string{"--version"}},
	{Name: "curl", Executable: "curl", Args: []string{"--version"}},
	{Name: "git", Executable: "git", Args: []string{"--version"}},
	{Name: "git-lfs", Executable: "git-lfs", Args: []string{"version"}},
	{Name: "gh", Executable: "gh", Args: []string{"--version"}},
	{Name: "ripgrep", Executable: "rg", Args: []string{"--version"}},
	{Name: "fd", Executable: "fd", Args: []string{"--version"}},
	{Name: "jq", Executable: "jq", Args: []string{"--version"}},
	{Name: "yq", Executable: "yq", Args: []string{"--version"}, Version: YQVersion},
	{Name: "bat", Executable: "bat", Args: []string{"--version"}},
	{Name: "file", Executable: "file", Args: []string{"--version"}},
	{Name: "make", Executable: "make", Args: []string{"--version"}},
	{Name: "go", Executable: "go", Args: []string{"version"}, Version: "go" + GoVersion},
	{Name: "python", Executable: "python3", Args: []string{"--version"}},
	{Name: "node", Executable: "node", Args: []string{"--version"}, Version: "v" + NodeVersion},
	{Name: "npm", Executable: "npm", Args: []string{"--version"}},
	{Name: "pnpm", Executable: "pnpm", Args: []string{"--version"}, Version: PNPMVersion},
	{Name: "codex", Executable: "codex", Args: []string{"--version"}, Version: CodexVersion},
	{Name: "playwright", Executable: "node", Args: []string{"-e", playwrightProbe}, Version: PlaywrightVersion},
}

var workstationTools = []Tool{
	{Name: "bash", Executable: "bash", Args: []string{"--version"}},
	{Name: "curl", Executable: "curl", Args: []string{"--version"}},
	{Name: "git", Executable: "git", Args: []string{"--version"}},
	{Name: "gh", Executable: "gh", Args: []string{"--version"}},
	{Name: "jq", Executable: "jq", Args: []string{"--version"}},
	{Name: "ssh-keygen", Executable: "ssh-keygen"},
}

const playwrightProbe = `const p=require('playwright/package.json'); if(p.version!==process.argv[1]) process.exit(2); const {chromium}=require('playwright'); (async()=>{const b=await chromium.launch({headless:true}); await b.close(); console.log(p.version)})().catch(e=>{console.error(e.message);process.exit(1)})`

var debianPackages = []string{
	"bash", "bat", "build-essential", "ca-certificates", "coreutils", "curl",
	"diffutils", "fd-find", "file", "findutils", "gawk", "gh", "git", "git-lfs",
	"grep", "jq", "less", "lsof", "make", "openssh-client", "patch", "pkg-config",
	"procps", "python3", "python3-pip", "python3-venv", "ripgrep", "tar",
	"rsync", "sed", "tree", "unzip", "util-linux", "xz-utils", "zip",
}

// RequiredTools returns a copy of the coding-agent tool contract.
func RequiredTools() []Tool {
	return copyTools(requiredTools)
}

// RequiredWorkstationTools returns the tools needed to operate hosted Vessica
// from a developer workstation. The larger worker contract belongs in the
// managed Railway checkpoint and must not make hosted workstation readiness
// depend on Linux-only utilities or exact build-tool versions.
func RequiredWorkstationTools() []Tool {
	return copyTools(workstationTools)
}

func copyTools(source []Tool) []Tool {
	tools := make([]Tool, len(source))
	for index, tool := range source {
		tool.Args = append([]string(nil), tool.Args...)
		tools[index] = tool
	}
	return tools
}

// Verify checks the complete contract using the caller's PATH and environment.
func Verify(ctx context.Context, workdir string) []Check {
	return verifyTools(ctx, workdir, requiredTools)
}

// VerifyWorkstation checks only the hosted workstation contract.
func VerifyWorkstation(ctx context.Context, workdir string) []Check {
	return verifyTools(ctx, workdir, workstationTools)
}

func verifyTools(ctx context.Context, workdir string, tools []Tool) []Check {
	checks := make([]Check, 0, len(tools))
	env := WithGlobalNodePath(os.Environ())
	for _, tool := range tools {
		check := Check{Name: tool.Name, Executable: tool.Executable}
		path, err := exec.LookPath(tool.Executable)
		if err != nil {
			check.Error = tool.Executable + " not found in PATH"
			checks = append(checks, check)
			continue
		}
		if len(tool.Args) == 0 {
			check.OK = true
			check.Version = path
			checks = append(checks, check)
			continue
		}
		args := append([]string(nil), tool.Args...)
		if tool.Name == "playwright" {
			args = append(args, tool.Version)
		}
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Env = env
		if workdir != "" {
			cmd.Dir = workdir
		}
		out, runErr := cmd.CombinedOutput()
		detail := firstLine(string(out))
		if runErr != nil {
			check.Error = strings.TrimSpace(detail)
			if check.Error == "" {
				check.Error = runErr.Error()
			}
			checks = append(checks, check)
			continue
		}
		if tool.Version != "" && !versionMatches(tool, detail) {
			check.Error = fmt.Sprintf("version mismatch: want %s, got %s", tool.Version, detail)
			checks = append(checks, check)
			continue
		}
		check.OK = true
		check.Version = detail
		checks = append(checks, check)
	}
	return checks
}

func versionMatches(tool Tool, output string) bool {
	if tool.Name == "pnpm" || tool.Name == "playwright" {
		return strings.TrimSpace(output) == tool.Version
	}
	return strings.Contains(output, tool.Version)
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}

// AllOK reports whether every required tool passed verification.
func AllOK(checks []Check) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

// Fingerprint identifies the immutable contents of a worker checkpoint.
func Fingerprint() string {
	packages := append([]string(nil), debianPackages...)
	sort.Strings(packages)
	parts := []string{
		ContractVersion, CodexVersion, PlaywrightVersion, PNPMVersion,
		NodeVersion, NodeAMD64SHA256, NodeARM64SHA256,
		GoVersion, GoAMD64SHA256, GoARM64SHA256,
		YQVersion, YQAMD64SHA256, YQARM64SHA256, strings.Join(packages, ","),
	}
	for _, tool := range requiredTools {
		parts = append(parts, tool.Name, tool.Executable, strings.Join(tool.Args, " "), tool.Version)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])[:12]
}

// CheckpointInstallCommand installs and validates the hosted worker toolchain.
func CheckpointInstallCommand() string {
	packages := append([]string(nil), debianPackages...)
	sort.Strings(packages)
	return strings.Join([]string{
		"set -euo pipefail",
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update",
		"apt-get install -y --no-install-recommends " + strings.Join(packages, " "),
		"rm -rf /var/lib/apt/lists/*",
		"ln -sf \"$(command -v fdfind)\" /usr/local/bin/fd",
		"ln -sf \"$(command -v batcat)\" /usr/local/bin/bat",
		NodeInstallCommand(),
		GoInstallCommand(),
		YQInstallCommand(),
		"id -u vessica-agent >/dev/null 2>&1 || useradd --create-home --shell /bin/bash vessica-agent",
		"export NPM_CONFIG_PREFIX=/usr/local",
		"npm install -g pnpm@" + PNPMVersion + " @openai/codex@" + CodexVersion + " playwright@" + PlaywrightVersion,
		"export NODE_PATH=/usr/local/lib/node_modules",
		"install -d -m 0755 /opt/ms-playwright",
		"export PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright",
		"playwright install --with-deps chromium",
		"chmod -R a+rX /opt/ms-playwright",
		AgentShellVerifyCommand(),
	}, "\n")
}

// ShellVerifyCommand verifies the contract without needing the ves binary.
func ShellVerifyCommand() string {
	commands := make([]string, 0, len(requiredTools)+2)
	for _, tool := range requiredTools {
		switch tool.Name {
		case "playwright":
			commands = append(commands, "node -e "+shellQuote(playwrightProbe)+" "+shellQuote(PlaywrightVersion)+" >/dev/null")
		case "pnpm":
			commands = append(commands, "test \"$(pnpm --version)\" = "+shellQuote(PNPMVersion))
		case "codex":
			commands = append(commands, "codex --version | grep -F "+shellQuote(CodexVersion)+" >/dev/null")
		case "yq":
			commands = append(commands, "yq --version | grep -F "+shellQuote(YQVersion)+" >/dev/null")
		case "node", "go":
			commands = append(commands, tool.Executable+" "+strings.Join(tool.Args, " ")+" | grep -F "+shellQuote(tool.Version)+" >/dev/null")
		default:
			commands = append(commands, "command -v "+shellQuote(tool.Executable)+" >/dev/null")
		}
	}
	return strings.Join(commands, " && ") + " || { echo 'Railway worker checkpoint does not satisfy the Vessica toolchain contract' >&2; exit 1; }"
}

// NodeInstallCommand installs checksum-pinned Node and its bundled npm.
func NodeInstallCommand() string {
	return strings.Join([]string{
		"case \"$(uname -m)\" in",
		"  x86_64|amd64) node_arch=x64; node_sha=" + NodeAMD64SHA256 + " ;;",
		"  aarch64|arm64) node_arch=arm64; node_sha=" + NodeARM64SHA256 + " ;;",
		"  *) echo \"unsupported Node architecture: $(uname -m)\" >&2; exit 1 ;;",
		"esac",
		"curl -fsSLo /tmp/node.tar.xz https://nodejs.org/dist/v" + NodeVersion + "/node-v" + NodeVersion + "-linux-${node_arch}.tar.xz",
		"echo \"${node_sha}  /tmp/node.tar.xz\" | sha256sum -c -",
		"rm -rf /opt/node-v" + NodeVersion,
		"install -d -m 0755 /opt/node-v" + NodeVersion,
		"tar -xJf /tmp/node.tar.xz --strip-components=1 -C /opt/node-v" + NodeVersion,
		"for binary in node npm npx corepack; do ln -sf /opt/node-v" + NodeVersion + "/bin/${binary} /usr/local/bin/${binary}; done",
		"rm -f /tmp/node.tar.xz",
	}, "\n")
}

// GoInstallCommand installs the repository-compatible Go toolchain.
func GoInstallCommand() string {
	return strings.Join([]string{
		"case \"$(uname -m)\" in",
		"  x86_64|amd64) go_arch=amd64; go_sha=" + GoAMD64SHA256 + " ;;",
		"  aarch64|arm64) go_arch=arm64; go_sha=" + GoARM64SHA256 + " ;;",
		"  *) echo \"unsupported Go architecture: $(uname -m)\" >&2; exit 1 ;;",
		"esac",
		"curl -fsSLo /tmp/go.tar.gz https://go.dev/dl/go" + GoVersion + ".linux-${go_arch}.tar.gz",
		"echo \"${go_sha}  /tmp/go.tar.gz\" | sha256sum -c -",
		"rm -rf /usr/local/go",
		"tar -xzf /tmp/go.tar.gz -C /usr/local",
		"ln -sf /usr/local/go/bin/go /usr/local/bin/go",
		"ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt",
		"rm -f /tmp/go.tar.gz",
	}, "\n")
}

// AgentShellVerifyCommand runs the shell contract with the coding agent's identity.
func AgentShellVerifyCommand() string {
	return "runuser --user vessica-agent --preserve-environment -- env HOME=/home/vessica-agent bash -c " + shellQuote(ShellVerifyCommand())
}

// YQInstallCommand returns the checksum-verified yq installation script.
func YQInstallCommand() string {
	return strings.Join([]string{
		"case \"$(uname -m)\" in",
		"  x86_64|amd64) yq_arch=amd64; yq_sha=" + YQAMD64SHA256 + " ;;",
		"  aarch64|arm64) yq_arch=arm64; yq_sha=" + YQARM64SHA256 + " ;;",
		"  *) echo \"unsupported yq architecture: $(uname -m)\" >&2; exit 1 ;;",
		"esac",
		"curl -fsSLo /tmp/yq https://github.com/mikefarah/yq/releases/download/v" + YQVersion + "/yq_linux_${yq_arch}",
		"echo \"${yq_sha}  /tmp/yq\" | sha256sum -c -",
		"install -m 0755 /tmp/yq /usr/local/bin/yq",
		"rm -f /tmp/yq",
	}, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// WithGlobalNodePath adds the global npm module directory when it is available.
func WithGlobalNodePath(env []string) []string {
	if os.Getenv("NODE_PATH") != "" {
		return env
	}
	cmd := exec.Command("npm", "root", "-g")
	out, err := cmd.Output()
	if err != nil {
		return env
	}
	return append(env, "NODE_PATH="+filepath.Clean(strings.TrimSpace(string(out))))
}
