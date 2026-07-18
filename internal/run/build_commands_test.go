package run

import (
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/harness"
)

func TestBuildRunsBeforeTests(t *testing.T) {
	commands := orderedBuildCommands(&harness.HarnessYAML{
		Build: harness.Build{Command: "build"},
		Test:  harness.Test{Command: "test"},
	}, "lint-arch")
	var buildIndex, testIndex = -1, -1
	for index, command := range commands {
		switch command.name {
		case "build":
			buildIndex = index
		case "test":
			testIndex = index
		}
	}
	if buildIndex < 0 || testIndex < 0 || buildIndex >= testIndex {
		t.Fatalf("command order=%#v", commands)
	}
}
