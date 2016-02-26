package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// buildtime and githash are being defined at linking.
var buildtime = "Unknown build time"
var githash = "Unknow hash"

var versionCommand = &cobra.Command{
	Use:   "version",
	Short: "print the current version",
	Run:   showVersion,
}

func showVersion(_ *cobra.Command, _ []string) {
	fmt.Printf("Build %s (%s)\n", githash, buildtime)
	os.Exit(0)
}
