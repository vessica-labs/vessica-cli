package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	data, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("encode Railway credentials: %w", err)
	}
	return auth.StoreSecret(railwaySecretsReference(root), data)
}

func loadRailwaySecrets(root string) (railwaySecrets, error) {
	var secrets railwaySecrets
	reference := railwaySecretsReference(root)
	data, err := auth.LoadSecret(reference)
	if err != nil {
		return secrets, err
	}
	if err := json.Unmarshal(data, &secrets); err == nil {
		return secrets, nil
	}
	decoded, decodeErr := hex.DecodeString(strings.TrimSpace(string(data)))
	if decodeErr != nil || json.Unmarshal(decoded, &secrets) != nil {
		return railwaySecrets{}, fmt.Errorf("decode stored Railway credentials: invalid credential record")
	}
	if err := saveRailwaySecrets(root, secrets); err != nil {
		return railwaySecrets{}, fmt.Errorf("repair stored Railway credentials: %w", err)
	}
	return secrets, nil
}

func loadOptionalRailwaySecrets(root string) (railwaySecrets, error) {
	secrets, err := loadRailwaySecrets(root)
	if err == nil {
		return secrets, nil
	}
	if auth.IsSecretNotFound(err) {
		return railwaySecrets{}, nil
	}
	return railwaySecrets{}, err
}

func initializeRailwaySecrets(secrets railwaySecrets, runtimeToken string) railwaySecrets {
	if secrets.ServiceToken == "" {
		secrets.ServiceToken = randomSecret(32)
	}
	if secrets.WorkerToken == "" {
		secrets.WorkerToken = randomSecret(32)
	}
	if secrets.WebhookSecret == "" {
		secrets.WebhookSecret = randomSecret(32)
	}
	if secrets.CredentialKey == "" {
		secrets.CredentialKey = base64.RawStdEncoding.EncodeToString(randomBytes(32))
	}
	if secrets.KnowledgeToken == "" {
		secrets.KnowledgeToken = randomSecret(32)
	}
	if secrets.KnowledgeAdminToken == "" {
		secrets.KnowledgeAdminToken = randomSecret(32)
	}
	if secrets.ControlDatabasePassword == "" {
		secrets.ControlDatabasePassword = randomSecret(32)
	}
	if secrets.KnowledgeDatabasePassword == "" {
		secrets.KnowledgeDatabasePassword = randomSecret(32)
	}
	secrets.RuntimeToken = runtimeToken
	return secrets
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
