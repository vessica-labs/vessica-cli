package codexplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
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
