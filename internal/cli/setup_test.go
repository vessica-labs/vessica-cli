package cli

import "testing"

func TestSetupPluginFlagsAreAvailableForCodexAndClaude(t *testing.T) {
	cmd := newSetupCmd(&App{})
	for _, runner := range []string{"codex", "claude"} {
		subcommand, _, err := cmd.Find([]string{runner})
		if err != nil {
			t.Fatal(err)
		}
		if subcommand.Flags().Lookup("plugin") == nil {
			t.Fatalf("setup %s is missing --plugin", runner)
		}
		if subcommand.Flags().Lookup("check") == nil {
			t.Fatalf("setup %s is missing --check", runner)
		}
	}
}

func TestSetupPluginFlagsRemainUnavailableForGuidanceOnlyTargets(t *testing.T) {
	cmd := newSetupCmd(&App{})
	for _, runner := range []string{"cursor", "pi", "mcp"} {
		subcommand, _, err := cmd.Find([]string{runner})
		if err != nil {
			t.Fatal(err)
		}
		if subcommand.Flags().Lookup("plugin") != nil || subcommand.Flags().Lookup("check") != nil {
			t.Fatalf("setup %s unexpectedly exposes plugin flags", runner)
		}
	}
}
