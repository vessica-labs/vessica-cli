package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/vessica-labs/vessica-cli/internal/cli"
	"github.com/vessica-labs/vessica-cli/internal/output"
	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func main() {
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		if output.IsPrinted(err) {
			os.Exit(output.ExitCode(err))
		}
		jsonMode := false
		if f := root.PersistentFlags().Lookup("json"); f != nil && f.Value.String() == "true" {
			jsonMode = true
		}
		if jsonMode {
			var ke *knowledge.Error
			if errors.As(err, &ke) {
				_ = output.PrintError(os.Stderr, ke.Code, ke.Message, "")
				mapped := &output.Error{Body: output.ErrorBody{Code: ke.Code, Message: ke.Message}}
				os.Exit(output.ExitCode(mapped))
			}
			_ = output.PrintError(os.Stderr, "command_failed", err.Error(), "")
		} else {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(output.ExitCode(err))
	}
}
