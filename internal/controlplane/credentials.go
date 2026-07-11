package controlplane

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type CredentialManager struct {
	DB  *state.DB
	key []byte
	mu  sync.Mutex
}

func (m *CredentialManager) Has(ctx context.Context, provider string) bool {
	_, err := m.DB.GetHostedCredential(ctx, provider)
	return err == nil
}

func NewCredentialManager(ctx context.Context, db *state.DB, encodedKey string, initial map[string]string) (*CredentialManager, error) {
	key, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("VES_CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	manager := &CredentialManager{DB: db, key: key}
	for provider, raw := range initial {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := db.GetHostedCredential(ctx, provider); err == nil {
			continue
		}
		if _, err := auth.UnmarshalOAuth(raw); err != nil {
			return nil, fmt.Errorf("decode initial %s OAuth credential: %w", provider, err)
		}
		if err := manager.persist(ctx, provider, []byte(raw)); err != nil {
			return nil, err
		}
	}
	return manager, nil
}

func (m *CredentialManager) Token(ctx context.Context, provider string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	encrypted, err := m.DB.GetHostedCredential(ctx, provider)
	if err != nil {
		return "", err
	}
	raw, err := m.decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt %s credential: %w", provider, err)
	}
	credential, err := auth.UnmarshalOAuth(string(raw))
	if err != nil {
		return "", err
	}
	before := credential.UpdatedAt
	credential, err = auth.RefreshIfNeeded(ctx, credential)
	if err != nil {
		return "", fmt.Errorf("refresh %s credential: %w", provider, err)
	}
	if !credential.UpdatedAt.Equal(before) {
		updated, _ := json.Marshal(credential)
		if err := m.persist(ctx, provider, updated); err != nil {
			return "", err
		}
	}
	return credential.AccessToken, nil
}

func (m *CredentialManager) persist(ctx context.Context, provider string, raw []byte) error {
	encrypted, err := m.encrypt(raw)
	if err != nil {
		return err
	}
	return m.DB.PutHostedCredential(ctx, provider, encrypted)
}

func (m *CredentialManager) encrypt(plain []byte) (string, error) {
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (m *CredentialManager) decrypt(encoded string) ([]byte, error) {
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted credential is truncated")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
