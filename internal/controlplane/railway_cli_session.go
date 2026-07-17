package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const railwayCLISessionCredential = "railway_cli_session"

type railwayCLISessionBundle struct {
	Config     []byte `json:"config"`
	PrivateKey []byte `json:"private_key"`
	PublicKey  []byte `json:"public_key"`
}

// RailwayCLISession owns the official Railway CLI's device-authorized session
// and the dedicated SSH key used for sandbox forwarding. The durable copy is
// encrypted by CredentialManager; files are materialized only in a private
// control-plane home so the Railway CLI can refresh its own token atomically.
type RailwayCLISession struct {
	Credentials   *CredentialManager
	CLIPath       string
	SSHKeygenPath string
	Home          string
	WorkspaceID   string
	mu            sync.Mutex
}

func NewRailwayCLISession(credentials *CredentialManager, cliPath, home, workspaceID string) *RailwayCLISession {
	if strings.TrimSpace(cliPath) == "" {
		cliPath = "railway"
	}
	return &RailwayCLISession{
		Credentials: credentials, CLIPath: cliPath, SSHKeygenPath: "ssh-keygen",
		Home: strings.TrimSpace(home), WorkspaceID: strings.TrimSpace(workspaceID),
	}
}

func (s *RailwayCLISession) configPath() string {
	return filepath.Join(s.Home, ".railway", "config.json")
}

func (s *RailwayCLISession) keyPath() string {
	return filepath.Join(s.Home, ".ssh", "id_ed25519_vessica")
}

func (s *RailwayCLISession) Ready() bool {
	if s == nil || s.Home == "" {
		return false
	}
	return validRailwayCLIConfig(s.configPath()) == nil && validPrivateFile(s.keyPath()) == nil
}

// Restore materializes the encrypted session after a deployment restart.
func (s *RailwayCLISession) Restore(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Credentials == nil {
		return false, fmt.Errorf("credential manager is required")
	}
	if !s.Credentials.Has(ctx, railwayCLISessionCredential) {
		return false, nil
	}
	raw, err := s.Credentials.LoadOpaque(ctx, railwayCLISessionCredential)
	if err != nil {
		return false, fmt.Errorf("load Railway CLI session: %w", err)
	}
	var bundle railwayCLISessionBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return false, fmt.Errorf("decode Railway CLI session: %w", err)
	}
	if err := validateRailwayCLIBundle(bundle); err != nil {
		return false, err
	}
	if err := writeCredentialFile(s.configPath(), bundle.Config, 0o600); err != nil {
		return false, err
	}
	if err := writeCredentialFile(s.keyPath(), bundle.PrivateKey, 0o600); err != nil {
		return false, err
	}
	if err := writeCredentialFile(s.keyPath()+".pub", bundle.PublicKey, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// Persist snapshots the CLI's potentially rotated refresh token and key into
// encrypted durable state. It is safe to call after every Railway command.
func (s *RailwayCLISession) Persist(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked(ctx)
}

func (s *RailwayCLISession) persistLocked(ctx context.Context) error {
	if s.Credentials == nil {
		return fmt.Errorf("credential manager is required")
	}
	configRaw, err := os.ReadFile(s.configPath())
	if err != nil {
		return fmt.Errorf("read Railway CLI session: %w", err)
	}
	privateKey, err := os.ReadFile(s.keyPath())
	if err != nil {
		return fmt.Errorf("read Railway forwarding key: %w", err)
	}
	publicKey, err := os.ReadFile(s.keyPath() + ".pub")
	if err != nil {
		return fmt.Errorf("read Railway forwarding public key: %w", err)
	}
	bundle := railwayCLISessionBundle{Config: configRaw, PrivateKey: privateKey, PublicKey: publicKey}
	if err := validateRailwayCLIBundle(bundle); err != nil {
		return err
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode Railway CLI session: %w", err)
	}
	if err := s.Credentials.StoreOpaque(ctx, railwayCLISessionCredential, raw); err != nil {
		return fmt.Errorf("persist Railway CLI session: %w", err)
	}
	return nil
}

// Authorize runs Railway's device-code login inside the control plane, creates
// a dedicated key locally, registers only its public half, and persists the
// resulting session. The caller must relay stdout promptly so the human can
// approve the short-lived device authorization.
func (s *RailwayCLISession) Authorize(ctx context.Context, stdout, stderr io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Home == "" {
		return fmt.Errorf("Railway CLI home is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath()), 0o700); err != nil {
		return fmt.Errorf("create Railway CLI config directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.keyPath()), 0o700); err != nil {
		return fmt.Errorf("create Railway SSH directory: %w", err)
	}
	if err := s.run(ctx, stdout, stderr, s.CLIPath, "login", "--browserless"); err != nil {
		return fmt.Errorf("authorize Railway CLI session: %w", err)
	}
	if err := os.Chmod(s.configPath(), 0o600); err != nil {
		return fmt.Errorf("secure Railway CLI session: %w", err)
	}
	if err := s.ensureForwardingKeyLocked(ctx, stdout, stderr); err != nil {
		return err
	}
	if err := s.run(ctx, io.Discard, stderr, s.CLIPath, "whoami", "--json"); err != nil {
		return fmt.Errorf("validate Railway CLI session: %w", err)
	}
	return s.persistLocked(ctx)
}

// RotateForwardingKey replaces a key that was registered in the wrong Railway
// key bucket without requiring another device authorization. Railway prevents
// the same public key from being registered both to a workspace and a user, so
// recovery must use a new fingerprint.
func (s *RailwayCLISession) RotateForwardingKey(ctx context.Context, stdout, stderr io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Ready() {
		return fmt.Errorf("Railway CLI session is not authorized")
	}
	if err := os.Remove(s.keyPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Railway forwarding key: %w", err)
	}
	if err := os.Remove(s.keyPath() + ".pub"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Railway forwarding public key: %w", err)
	}
	if err := s.ensureForwardingKeyLocked(ctx, stdout, stderr); err != nil {
		return err
	}
	return s.persistLocked(ctx)
}

func (s *RailwayCLISession) ensureForwardingKeyLocked(ctx context.Context, stdout, stderr io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(s.keyPath()), 0o700); err != nil {
		return fmt.Errorf("create Railway SSH directory: %w", err)
	}
	if _, err := os.Stat(s.keyPath()); os.IsNotExist(err) {
		if err := s.run(ctx, io.Discard, stderr, s.SSHKeygenPath,
			"-q", "-t", "ed25519", "-N", "", "-C", "vessica-control-plane", "-f", s.keyPath()); err != nil {
			return fmt.Errorf("generate Railway forwarding key: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect Railway forwarding key: %w", err)
	}
	if err := os.Chmod(s.keyPath(), 0o600); err != nil {
		return fmt.Errorf("secure Railway forwarding key: %w", err)
	}
	var fingerprintOutput bytes.Buffer
	if err := s.run(ctx, &fingerprintOutput, stderr, s.SSHKeygenPath, "-lf", s.keyPath()+".pub", "-E", "sha256"); err != nil {
		return fmt.Errorf("fingerprint Railway forwarding key: %w", err)
	}
	fingerprintFields := strings.Fields(fingerprintOutput.String())
	if len(fingerprintFields) < 2 || !strings.HasPrefix(fingerprintFields[1], "SHA256:") {
		return fmt.Errorf("fingerprint Railway forwarding key: unexpected ssh-keygen output")
	}
	fingerprint := fingerprintFields[1]
	registered, err := s.forwardingKeyRegisteredLocked(ctx, fingerprint, stderr)
	if err != nil {
		return err
	}
	if registered {
		return nil
	}
	if err := s.run(ctx, stdout, stderr, s.CLIPath, "ssh", "keys", "add",
		"--key", s.keyPath(), "--name", "vessica-control-plane"); err != nil {
		return fmt.Errorf("register Railway forwarding key: %w", err)
	}
	registered, err = s.forwardingKeyRegisteredLocked(ctx, fingerprint, stderr)
	if err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("Railway forwarding key was not registered for the CLI session")
	}
	return nil
}

func (s *RailwayCLISession) forwardingKeyRegisteredLocked(ctx context.Context, fingerprint string, stderr io.Writer) (bool, error) {
	var keyList bytes.Buffer
	if err := s.run(ctx, &keyList, stderr, s.CLIPath, "ssh", "keys", "list"); err != nil {
		return false, fmt.Errorf("verify Railway forwarding key: %w", err)
	}
	registered := keyList.String()
	if localIndex := strings.Index(registered, "Local Keys (not registered):"); localIndex >= 0 {
		registered = registered[:localIndex]
	}
	return strings.Contains(registered, fingerprint), nil
}

func (s *RailwayCLISession) Validate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Ready() {
		return fmt.Errorf("Railway CLI session is not authorized")
	}
	if err := s.run(ctx, io.Discard, io.Discard, s.CLIPath, "whoami", "--json"); err != nil {
		return fmt.Errorf("validate Railway CLI session: %w", err)
	}
	return s.persistLocked(ctx)
}

func (s *RailwayCLISession) run(ctx context.Context, stdout, stderr io.Writer, executable string, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "HOME=") || strings.HasPrefix(value, "RAILWAY_TOKEN=") || strings.HasPrefix(value, "RAILWAY_API_TOKEN=") {
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.Env = append(cmd.Env,
		"HOME="+s.Home,
		"RAILWAY_CALLER=vessica-control-plane",
		"RAILWAY_AGENT_SESSION=vessica-preview-session",
	)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func validRailwayCLIConfig(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var config struct {
		User struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("Railway CLI config is invalid: %w", err)
	}
	if strings.TrimSpace(config.User.AccessToken) == "" || strings.TrimSpace(config.User.RefreshToken) == "" {
		return fmt.Errorf("Railway CLI config does not contain a refreshable session")
	}
	return nil
}

func validPrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("credential file %s must not be accessible by group or others", filepath.Base(path))
	}
	return nil
}

func validateRailwayCLIBundle(bundle railwayCLISessionBundle) error {
	if len(bundle.Config) == 0 || len(bundle.PrivateKey) == 0 || len(bundle.PublicKey) == 0 {
		return fmt.Errorf("Railway CLI session bundle is incomplete")
	}
	var config struct {
		User struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"user"`
	}
	if err := json.Unmarshal(bundle.Config, &config); err != nil {
		return fmt.Errorf("Railway CLI session config is invalid: %w", err)
	}
	if strings.TrimSpace(config.User.AccessToken) == "" || strings.TrimSpace(config.User.RefreshToken) == "" {
		return fmt.Errorf("Railway CLI session is not refreshable")
	}
	if !strings.Contains(string(bundle.PrivateKey), "PRIVATE KEY") || !strings.HasPrefix(strings.TrimSpace(string(bundle.PublicKey)), "ssh-ed25519 ") {
		return fmt.Errorf("Railway forwarding key is invalid")
	}
	return nil
}

func writeCredentialFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
