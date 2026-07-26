package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/vessica-labs/vessica-cli/internal/claudeplugin"
)

func main() {
	dest := flag.String("dest", "", "destination directory for the rendered marketplace")
	cliVersion := flag.String("cli-version", "", "Vessica CLI version to pin")
	pluginVersion := flag.String("plugin-version", "", "Claude plugin version")
	flag.Parse()
	if *dest == "" || *cliVersion == "" || *pluginVersion == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := claudeplugin.RenderMarketplace(*dest, *cliVersion, *pluginVersion); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
