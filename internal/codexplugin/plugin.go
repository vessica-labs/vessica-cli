package codexplugin

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:assets
var assets embed.FS

type installResult struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Marketplace string `json:"marketplace"`
	Installed   bool   `json:"installed"`
}

func Install() (*installResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	// The personal marketplace lives at ~/.agents/plugins/marketplace.json,
	// but its local source paths are resolved from the personal marketplace
	// root (~), so ./plugins/vessica must be installed at ~/plugins/vessica.
	dest := filepath.Join(home, "plugins", "vessica")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	err = fs.WalkDir(assets, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "assets" {
			return nil
		}
		rel, err := filepath.Rel("assets", path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if filepath.Base(filepath.Dir(target)) == "scripts" || filepath.Ext(target) == ".sh" {
			mode = 0o755
		}
		return os.WriteFile(target, body, mode)
	})
	if err != nil {
		return nil, err
	}
	marketplace := filepath.Join(home, ".agents", "plugins", "marketplace.json")
	if err := updateMarketplace(marketplace); err != nil {
		return nil, err
	}
	return &installResult{Name: "vessica", Path: dest, Marketplace: marketplace, Installed: true}, nil
}

func Status() map[string]any {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "plugins", "vessica", ".codex-plugin", "plugin.json")
	_, err := os.Stat(path)
	return map[string]any{"plugin": "vessica", "installed": err == nil, "manifest": path, "marketplace": filepath.Join(home, ".agents", "plugins", "marketplace.json")}
}

func updateMarketplace(path string) error {
	root := map[string]any{"name": "personal", "interface": map[string]any{"displayName": "Personal"}, "plugins": []any{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &root); err != nil {
			return fmt.Errorf("decode Codex marketplace: %w", err)
		}
	}
	plugins, _ := root["plugins"].([]any)
	entry := map[string]any{"name": "vessica", "source": map[string]any{"source": "local", "path": "./plugins/vessica"}, "policy": map[string]any{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Productivity"}
	replaced := false
	for i, raw := range plugins {
		if item, ok := raw.(map[string]any); ok && item["name"] == "vessica" {
			plugins[i] = entry
			replaced = true
		}
	}
	if !replaced {
		plugins = append(plugins, entry)
	}
	root["plugins"] = plugins
	if _, ok := root["interface"]; !ok {
		root["interface"] = map[string]any{"displayName": "Personal"}
	}
	body, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
