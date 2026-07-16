package codexplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if strings.Contains(string(bootstrap), "$base/checksums.txt") || !strings.Contains(string(bootstrap), "cli-checksums.txt") {
		t.Fatalf("bootstrap does not use the bundled checksum manifest: %s", bootstrap)
	}
	for _, name := range []string{"cli-version.txt", "cli-checksums.txt"} {
		if _, err := os.Stat(filepath.Join(result.Path, "scripts", name)); err != nil {
			t.Fatalf("missing bundled %s: %v", name, err)
		}
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
