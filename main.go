package main

import (
	"fmt"
	"os"

	"github.com/octoberswimmer/aer/flow2apex"
)

// version is overridden at link time via -ldflags.
var version = "dev"

func main() {
	cmd := flow2apex.NewCommand()
	if version != "" {
		cmd.Version = version
		cmd.SetVersionTemplate("flow2apex version {{.Version}}\n")
	}

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
