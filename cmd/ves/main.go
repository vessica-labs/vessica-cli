package main

import (
	"fmt"
	"os"

	"github.com/vessica-labs/vessica-cli/internal/cli"
	"github.com/vessica-labs/vessica-cli/internal/output"
)

func main() {
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		if output.IsPrinted(err) {
			os.Exit(1)
		}
		jsonMode := false
		if f := root.PersistentFlags().Lookup("json"); f != nil && f.Value.String() == "true" {
			jsonMode = true
		}
		if jsonMode {
			_ = output.PrintError(os.Stderr, "command_failed", err.Error(), "")
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(1)
	}
}
