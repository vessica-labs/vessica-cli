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

func TestWithGlobalNodePathPreservesConfiguredValue(t *testing.T) {
	t.Setenv("NODE_PATH", "/shared/modules")
	env := WithGlobalNodePath([]string{"PATH=" + os.Getenv("PATH"), "NODE_PATH=/shared/modules"})
	if strings.Join(env, "\n") != "PATH="+os.Getenv("PATH")+"\nNODE_PATH=/shared/modules" {
		t.Fatalf("environment changed: %v", env)
	}
}
