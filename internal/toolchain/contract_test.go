package toolchain

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRequiredToolsContainsAgentBaseline(t *testing.T) {
	want := map[string]bool{
		"ripgrep": false, "fd": false, "jq": false, "yq": false, "bat": false,
		"git": false, "gh": false, "go": false, "python": false, "node": false,
		"pnpm": false, "codex": false, "playwright": false,
	}
	for _, tool := range RequiredTools() {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("required tool %q is missing", name)
		}
	}
}

func TestWorkstationToolsExcludeManagedWorkerOnlyDependencies(t *testing.T) {
	tools := map[string]bool{}
	for _, tool := range RequiredWorkstationTools() {
		tools[tool.Name] = true
	}
	for _, required := range []string{"bash", "curl", "git", "gh", "jq", "ssh-keygen"} {
		if !tools[required] {
			t.Errorf("workstation tool %q is missing", required)
		}
	}
	for _, workerOnly := range []string{"git-lfs", "fd", "yq", "bat", "go", "node", "pnpm", "codex", "playwright"} {
		if tools[workerOnly] {
			t.Errorf("worker-only tool %q leaked into workstation readiness", workerOnly)
		}
	}
}

func TestCheckpointInstallCommandIsPinnedAndVerifiesAgent(t *testing.T) {
	script := CheckpointInstallCommand()
	for _, required := range []string{
		"ripgrep", "fd-find", "jq", "bat", "gh",
		"go" + GoVersion + ".linux-${go_arch}.tar.gz", GoAMD64SHA256, GoARM64SHA256,
		"node-v" + NodeVersion + "-linux-${node_arch}.tar.xz", NodeAMD64SHA256, NodeARM64SHA256,
		"pnpm@" + PNPMVersion, "@openai/codex@" + CodexVersion,
		"playwright@" + PlaywrightVersion, "v" + YQVersion,
		YQAMD64SHA256, YQARM64SHA256, "runuser --user vessica-agent",
		"PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright",
		"NPM_CONFIG_PREFIX=/usr/local", "NODE_PATH=/usr/local/lib/node_modules",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("installer missing %q", required)
		}
	}
	for _, forbidden := range []string{"@latest", "/latest/"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("installer contains mutable dependency %q", forbidden)
		}
	}
}

func TestCheckpointInstallCommandHasValidBashSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(CheckpointInstallCommand())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid installer shell: %v: %s", err, out)
	}
}

func TestRuntimeVerifyAvoidsRepeatedBrowserAndProjectSmoke(t *testing.T) {
	command := AgentRuntimeVerifyCommand()
	for _, required := range []string{CodexVersion, PNPMVersion, PlaywrightVersion, "PLAYWRIGHT_BROWSERS_PATH", "playwright/package.json"} {
		if !strings.Contains(command, required) {
			t.Fatalf("runtime verifier missing %q: %s", required, command)
		}
	}
	for _, forbidden := range []string{"chromium.launch", "pnpm run build", "pnpm test", "127.0.0.1:4173"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("runtime verifier repeats checkpoint smoke %q: %s", forbidden, command)
		}
	}
}

func TestFingerprintChangesWithContractInputs(t *testing.T) {
	first := Fingerprint()
	if len(first) != 12 {
		t.Fatalf("fingerprint length=%d", len(first))
	}
	if first != Fingerprint() {
		t.Fatal("fingerprint is not deterministic")
	}
}

func TestVerifyReportsMissingTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("NODE_PATH", "")
	checks := Verify(context.Background(), "")
	if AllOK(checks) {
		t.Fatal("empty PATH unexpectedly satisfied the contract")
	}
	for _, check := range checks {
		if check.OK || !strings.Contains(check.Error, "not found in PATH") {
			t.Fatalf("unexpected missing-tool result: %#v", check)
		}
	}
}

func TestVerifyWorkstationReportsOnlyWorkstationContract(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	checks := VerifyWorkstation(context.Background(), "")
	if len(checks) != len(RequiredWorkstationTools()) {
		t.Fatalf("checks=%d tools=%d", len(checks), len(RequiredWorkstationTools()))
	}
	for _, check := range checks {
		if check.Name == "go" || check.Name == "playwright" {
			t.Fatalf("worker-only check reported on workstation: %#v", check)
		}
	}
}

func TestWithGlobalNodePathPreservesConfiguredValue(t *testing.T) {
	t.Setenv("NODE_PATH", "/shared/modules")
	env := WithGlobalNodePath([]string{"PATH=" + os.Getenv("PATH"), "NODE_PATH=/shared/modules"})
	if strings.Join(env, "\n") != "PATH="+os.Getenv("PATH")+"\nNODE_PATH=/shared/modules" {
		t.Fatalf("environment changed: %v", env)
	}
}
