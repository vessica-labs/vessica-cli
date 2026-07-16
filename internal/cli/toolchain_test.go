package cli

import "testing"

func TestToolchainVerifyCommandIsRegistered(t *testing.T) {
	cmd, _, err := NewRoot().Find([]string{"toolchain", "verify"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.CommandPath() != "ves toolchain verify" {
		t.Fatalf("command path=%q", cmd.CommandPath())
	}
}
