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

func TestToolchainVerifyAcceptsWorkstationProfile(t *testing.T) {
	cmd, _, err := NewRoot().Find([]string{"toolchain", "verify", "--profile", "workstation"})
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flag("profile")
	if flag == nil || flag.DefValue != "worker" {
		t.Fatalf("profile flag=%v", flag)
	}
}
