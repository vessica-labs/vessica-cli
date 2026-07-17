package controlplane

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestRailwayCLISessionAuthorizePersistsAndRestoresEncryptedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := base64.RawStdEncoding.EncodeToString([]byte("12345678901234567890123456789012"))
	credentials, err := NewCredentialManager(ctx, db, key, nil)
	if err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(bin, "railway")
	cliScript := `#!/bin/sh
set -eu
printf '%s token=%s api=%s home=%s\n' "$*" "${RAILWAY_TOKEN:-}" "${RAILWAY_API_TOKEN:-}" "$HOME" >> "$VES_SESSION_LOG"
if [ "$1" = login ]; then
  mkdir -p "$HOME/.railway"
  printf '{"user":{"accessToken":"session-access","refreshToken":"session-refresh","tokenExpiresAt":4102444800}}' > "$HOME/.railway/config.json"
elif [ "$1" = whoami ]; then
  printf '{"id":"user"}\n'
elif [ "$1 $2 $3" = "ssh keys add" ]; then
  touch "$VES_SESSION_KEY_MARKER"
elif [ "$1 $2 $3" = "ssh keys list" ]; then
  if [ -f "$VES_SESSION_KEY_MARKER" ]; then
    printf 'Registered SSH Keys:\n  Fingerprint: SHA256:fixture\n'
  else
    printf 'Local Keys (not registered):\n  Fingerprint: SHA256:fixture\n'
  fi
fi
`
	if err := os.WriteFile(cli, []byte(cliScript), 0o755); err != nil {
		t.Fatal(err)
	}
	keygen := filepath.Join(bin, "ssh-keygen")
	keygenScript := `#!/bin/sh
set -eu
if [ "$1" = -lf ]; then
  printf '256 SHA256:fixture vessica-control-plane (ED25519)\n'
  exit 0
fi
while [ "$#" -gt 0 ]; do
  if [ "$1" = -f ]; then shift; path="$1"; fi
  shift
done
printf '%s\n' '-----BEGIN OPENSSH PRIVATE KEY-----' 'fixture' '-----END OPENSSH PRIVATE KEY-----' > "$path"
printf '%s\n' 'ssh-ed25519 AAAATEST vessica-control-plane' > "$path.pub"
`
	if err := os.WriteFile(keygen, []byte(keygenScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VES_SESSION_LOG", filepath.Join(root, "session.log"))
	t.Setenv("VES_SESSION_KEY_MARKER", filepath.Join(root, "key-registered"))
	t.Setenv("RAILWAY_TOKEN", "service-project-token")
	t.Setenv("RAILWAY_API_TOKEN", "service-api-token")
	home := filepath.Join(root, "home")
	session := NewRailwayCLISession(credentials, cli, home, "workspace-1")
	session.SSHKeygenPath = keygen
	var output bytes.Buffer
	if err := session.Authorize(ctx, &output, &output); err != nil {
		t.Fatal(err)
	}
	if !session.Ready() {
		t.Fatal("expected authorized session to be ready")
	}
	logRaw, err := os.ReadFile(filepath.Join(root, "session.log"))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logRaw)
	if strings.Contains(logText, "service-project-token") || strings.Contains(logText, "service-api-token") {
		t.Fatalf("service token leaked into CLI session: %s", logText)
	}
	if !strings.Contains(logText, "ssh keys add --key ") || strings.Contains(logText, "--workspace") || !strings.Contains(logText, "home="+home) {
		t.Fatalf("unexpected session commands: %s", logText)
	}
	if !strings.Contains(logText, "ssh keys list") {
		t.Fatalf("forwarding key registration was not verified: %s", logText)
	}
	encrypted, err := db.GetHostedCredential(ctx, railwayCLISessionCredential)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "session-access") || strings.Contains(encrypted, "PRIVATE KEY") {
		t.Fatal("Railway CLI session was not encrypted")
	}
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	restored := NewRailwayCLISession(credentials, cli, home, "workspace-1")
	ok, err := restored.Restore(ctx)
	if err != nil || !ok || !restored.Ready() {
		t.Fatalf("restored=%v ready=%v err=%v", ok, restored.Ready(), err)
	}
	for _, path := range []string{restored.configPath(), restored.keyPath()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o", path, info.Mode().Perm())
		}
	}
}
