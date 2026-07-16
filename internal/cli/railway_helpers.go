package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

func railwaySecretsReference(root string) string {
	absolute, _ := filepath.Abs(root)
	sum := sha256.Sum256([]byte(absolute))
	return "railway-workspace-" + hex.EncodeToString(sum[:8])
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
	data, _ := json.MarshalIndent(secrets, "", "  ")
	return auth.StoreSecret(railwaySecretsReference(root), data)
}

func loadRailwaySecrets(root string) (railwaySecrets, error) {
	var secrets railwaySecrets
	data, err := auth.LoadSecret(railwaySecretsReference(root))
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
