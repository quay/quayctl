// Copyright 2016 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/coreos/quayctl/engine"
)

var rootCommand = &cobra.Command{
	Use:   "quayctl",
	Short: "Quay cuddle",
	Long:  "Various utilities for working with the Quay container registry",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
		os.Exit(1)
	},
}

// addEngineCommands adds a command for each container engine to the root command, as well
// as generating the engine-specific commands.
func addEngineCommands(rootCommand *cobra.Command) {
	// Add each of the engines.
	engines := []engine.ContainerEngine{&engine.DockerEngine{}}
	for _, engine := range engines {
		engineCommand := &cobra.Command{
			Use:   engine.Name(),
			Short: engine.Title(),
			Long:  fmt.Sprintf("Invoke quayctl commands for %s", engine.Title()),
			Run: func(cmd *cobra.Command, args []string) {
				cmd.Usage()
				os.Exit(1)
			},
		}

		rootCommand.AddCommand(engineCommand)

		// Add the `torrent` commands to each of the engines.
		addTorrentCommands(engine, engineCommand)
	}
}

func init() {
	addEngineCommands(rootCommand)
	rootCommand.AddCommand(versionCommand)
}

func main() {
	if err := rootCommand.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
