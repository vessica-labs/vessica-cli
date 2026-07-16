package codexplugin

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/version"
)

func TestInstallWritesPluginAndMarketplaceEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	result, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(filepath.Join(result.Path, ".codex-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "skills", "use-knowledge", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "skills", "operate-vessica", "references", "operator-guide.md")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(result.Path, "scripts", "ensure-ves.sh")); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("bootstrap script is not executable: info=%v err=%v", info, err)
	}
	bootstrap, err := os.ReadFile(filepath.Join(result.Path, "scripts", "ensure-ves.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bootstrap), "$base/checksums.txt") || strings.Contains(string(bootstrap), "cli-checksums.txt") {
		t.Fatalf("bootstrap does not use the release checksum manifest: %s", bootstrap)
	}
	if strings.Contains(string(bootstrap), "command -v ves") {
		t.Fatalf("bootstrap trusts an unmanaged ves binary: %s", bootstrap)
	}
	pin, err := os.ReadFile(filepath.Join(result.Path, "scripts", "cli-version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(pin)) != version.Version {
		t.Fatalf("CLI pin=%q want=%q", strings.TrimSpace(string(pin)), version.Version)
	}
	raw, err := os.ReadFile(result.Marketplace)
	if err != nil {
		t.Fatal(err)
	}
	var marketplace struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &marketplace); err != nil {
		t.Fatal(err)
	}
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != "vessica" {
		t.Fatalf("marketplace=%s", raw)
	}
	if _, err := Install(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(result.Marketplace)
	_ = json.Unmarshal(raw, &marketplace)
	if len(marketplace.Plugins) != 1 {
		t.Fatalf("duplicate entries: %s", raw)
	}
}

func TestBootstrapInstallsVerifiedManagedCLIAndReusesStampedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bootstrap is a POSIX shell script")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	result, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	target := runtime.GOOS + "_" + runtime.GOARCH
	asset := fmt.Sprintf("vessica-cli_%s_%s.tar.gz", version.Version, target)
	releaseDir := filepath.Join(home, "release")
	binDir := filepath.Join(home, "bin")
	installDir := filepath.Join(home, "managed")
	for _, dir := range []string{releaseDir, binDir, installDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	vesLog := filepath.Join(home, "ves.log")
	unmanagedLog := filepath.Join(home, "unmanaged.log")
	managedVES := fmt.Sprintf(`#!/bin/sh
if [ "${1:-}" = "--json" ] && [ "${2:-}" = "version" ]; then
  printf '{"schema":"vessica.cli/v1","ok":true,"data":{"version":"%s"}}'
  exit 0
fi
printf '%%s\n' "$*" >> "$VES_TEST_VES_LOG"
`, version.Version)
	archivePath := filepath.Join(releaseDir, asset)
	writeTestArchive(t, archivePath, "vessica-cli_"+version.Version+"_"+target+"/ves", []byte(managedVES), 0o755)
	archiveBody, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archiveBody)
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(fmt.Sprintf("%x  ./%s\n", digest, asset)), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
cp "$VES_TEST_RELEASE_DIR/$(basename "$url")" "$out"
`)
	writeExecutable(t, filepath.Join(binDir, "railway"), "#!/bin/sh\nprintf 'railway 5.0.0\\n'\n")
	writeExecutable(t, filepath.Join(binDir, "ves"), "#!/bin/sh\nprintf 'unmanaged\\n' >> \"$VES_TEST_UNMANAGED_LOG\"\n")
	bootstrap := filepath.Join(result.Path, "scripts", "ensure-ves.sh")
	run := func() {
		cmd := exec.Command("bash", bootstrap, "capabilities", "--json")
		cmd.Env = append(os.Environ(),
			"PATH="+binDir+string(os.PathListSeparator)+"/usr/bin:/bin",
			"VES_BIN_DIR="+installDir,
			"VES_TEST_RELEASE_DIR="+releaseDir,
			"VES_TEST_VES_LOG="+vesLog,
			"VES_TEST_UNMANAGED_LOG="+unmanagedLog,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bootstrap failed: %v: %s", err, output)
		}
	}
	run()
	if _, err := os.Stat(filepath.Join(installDir, "ves")); err != nil {
		t.Fatalf("managed CLI missing: %v", err)
	}
	stamps, err := filepath.Glob(filepath.Join(installDir, ".ves-*.sha256"))
	if err != nil || len(stamps) != 1 {
		t.Fatalf("verification stamp=%v err=%v", stamps, err)
	}
	if _, err := os.Stat(unmanagedLog); !os.IsNotExist(err) {
		t.Fatalf("unmanaged PATH binary was executed: %v", err)
	}
	if err := os.RemoveAll(releaseDir); err != nil {
		t.Fatal(err)
	}
	run()
	logBody, err := os.ReadFile(vesLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(logBody), "capabilities --json") != 2 {
		t.Fatalf("managed CLI invocations=%q", logBody)
	}
}

func writeTestArchive(t *testing.T, path, name string, body []byte, mode int64) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(tw, strings.NewReader(string(body))); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
