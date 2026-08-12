package redaction

import (
	"encoding/json"
	"testing"
)

func TestRedact(t *testing.T) {
	in := "token: ghp_abcdefghijklmnopqrstuvwxyz123456 password=supersecret VES_CODEX_AUTH_B64=opaque-login VES_RAILWAY_OAUTH_JSON=oauth-json VES_CREDENTIAL_ENCRYPTION_KEY=encrypt-key Authorization: Bearer secret-value"
	out := Redact(in)
	if out == in {
		t.Fatal("expected redaction")
	}
	if contains(out, "ghp_") || contains(out, "supersecret") || contains(out, "opaque-login") || contains(out, "oauth-json") || contains(out, "encrypt-key") || contains(out, "secret-value") {
		t.Fatalf("leaked secret: %s", out)
	}
}

func TestRedactMCPMaterialsAndPreserveJSON(t *testing.T) {
	in := `{"client":"vmc_clientsecret","access":"vma_accesssecret","refresh":"vmr_refreshsecret","claim":"triggerclaim_claimsecret","mcp":"mcpclaim_claimsecret","ClaimToken":"triggerclaim_nested"}`
	out := Redact(in)
	for _, leaked := range []string{"vmc_", "vma_", "vmr_", "triggerclaim_", "mcpclaim_"} {
		if contains(out, leaked) {
			t.Fatalf("leaked %q in %s", leaked, out)
		}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("redaction corrupted JSON: %v: %s", err, out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
