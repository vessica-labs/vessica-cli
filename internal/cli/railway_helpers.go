package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func railwaySecretsPath(root string) string {
	return filepath.Join(root, config.DirName, "secrets", "railway.json")
}

func railwaySSHPrivateKeyPath(root string) string {
	return filepath.Join(root, config.DirName, "secrets", "railway_ed25519")
}

func railwaySSHUserKeyPath(cfg config.Config) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if len(cfg.Hosted.ProjectID) < 8 {
		return "", fmt.Errorf("invalid Railway project id")
	}
	return filepath.Join(home, ".ssh", "vessica_control_plane_"+cfg.Hosted.ProjectID[:8]), nil
}

func saveRailwaySecrets(root string, secrets railwaySecrets) error {
	if err := os.MkdirAll(filepath.Dir(railwaySecretsPath(root)), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(secrets, "", "  ")
	return os.WriteFile(railwaySecretsPath(root), data, 0o600)
}

func loadRailwaySecrets(root string) (railwaySecrets, error) {
	var secrets railwaySecrets
	data, err := os.ReadFile(railwaySecretsPath(root))
	if err != nil {
		return secrets, err
	}
	return secrets, json.Unmarshal(data, &secrets)
}

func randomSecret(bytesCount int) string {
	return hex.EncodeToString(randomBytes(bytesCount))
}

func randomBytes(bytesCount int) []byte {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func moduleIsVessica(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	return err == nil && strings.Contains(string(data), "module github.com/vessica-labs/vessica-cli")
}
