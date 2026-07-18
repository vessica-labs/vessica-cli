package run

import "github.com/vessica-labs/vessica-cli/internal/harness"

type namedBuildCommand struct {
	name string
	cmd  string
}

func orderedBuildCommands(hy *harness.HarnessYAML, lintArch string) []namedBuildCommand {
	return []namedBuildCommand{
		{name: "lint", cmd: hy.Lint.Command},
		{name: "lint-arch", cmd: "bash " + shellQuote(lintArch)},
		{name: "build", cmd: hy.Build.Command},
		{name: "test", cmd: hy.Test.Command},
	}
}
