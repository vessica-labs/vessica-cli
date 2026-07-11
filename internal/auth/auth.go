package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

type Status struct {
	Provider  string `json:"provider"`
	LoggedIn  bool   `json:"logged_in"`
	Valid     bool   `json:"valid"`
	Error     string `json:"error,omitempty"`
	Account   string `json:"account,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type meta struct {
	Account   string `json:"account"`
	UpdatedAt string `json:"updated_at"`
}

func secretsDir() (string, error) {
	ud, err := config.UserDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(ud, "secrets"), nil
}

func tokenPath(provider string) (string, error) {
	dir, err := secretsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".token"), nil
}

func metaPath(provider string) (string, error) {
	dir, err := secretsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".meta.json"), nil
}

// Login stores a provider token (never committed to repo).
func Login(provider, token, account string) error {
	provider = strings.ToLower(provider)
	tp, err := tokenPath(provider)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tp, []byte(strings.TrimSpace(token)+"\n"), 0o600); err != nil {
		return err
	}
	mp, err := metaPath(provider)
	if err != nil {
		return err
	}
	m := meta{Account: account, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(mp, b, 0o600)
}

func Logout(provider string) error {
	provider = strings.ToLower(provider)
	_ = DeleteOAuth(provider)
	tp, _ := tokenPath(provider)
	mp, _ := metaPath(provider)
	_ = os.Remove(tp)
	_ = os.Remove(mp)
	return nil
}

func Token(provider string) (string, error) {
	provider = strings.ToLower(provider)
	if credential, err := LoadOAuth(provider); err == nil {
		updatedAt := credential.UpdatedAt
		credential, err = RefreshIfNeeded(context.Background(), credential)
		if err != nil {
			return "", fmt.Errorf("refresh %s login: %w", provider, err)
		}
		if !credential.UpdatedAt.Equal(updatedAt) {
			if err := SaveOAuth(credential); err != nil {
				return "", err
			}
		}
		return credential.AccessToken, nil
	}
	tp, err := tokenPath(provider)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(tp)
	if err != nil {
		return "", fmt.Errorf("not logged in to %s", provider)
	}
	return strings.TrimSpace(string(b)), nil
}

func StatusAll(providers []string) []Status {
	var out []Status
	for _, p := range providers {
		s := Status{Provider: p}
		if p == "openai" || p == "codex" {
			loggedIn, detail := CodexStatus(context.Background())
			s.LoggedIn, s.Valid = loggedIn, loggedIn
			if loggedIn {
				s.Account = detail
			} else if detail != "" {
				s.Error = detail
			}
			out = append(out, s)
			continue
		}
		if tok, err := Token(p); err == nil && tok != "" {
			s.LoggedIn = true
			s.Valid = true
			if mp, err := metaPath(p); err == nil {
				if b, err := os.ReadFile(mp); err == nil {
					var m meta
					_ = json.Unmarshal(b, &m)
					s.Account = m.Account
					s.UpdatedAt = m.UpdatedAt
				}
			}
			if p == "github" {
				if account, err := ValidateGitHubToken(tok); err != nil {
					s.Valid = false
					s.Error = err.Error()
				} else if s.Account == "" {
					s.Account = account
				}
			}
		}
		out = append(out, s)
	}
	return out
}

func ValidateGitHubToken(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github token validation failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var body struct {
		Login string `json:"login"`
	}
	_ = json.Unmarshal(data, &body)
	return body.Login, nil
}
