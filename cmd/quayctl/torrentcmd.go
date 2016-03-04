// Copyright 2015 CoreOS, Inc.
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
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/coreos/quayctl/bittorrent"
)

var (
	torrentFingerprint          bittorrent.ClientFingerprint
	torrentFolder               string
	torrentLowerPort            int
	torrentUpperPort            int
	torrentConnectionsPerSecond int
	torrentMaxDowloadRate       int
	torrentMaxUploadRate        int
	torrentSeedDuration         time.Duration
	torrentEncryptionMode       int
	torrentDebug                bool
	insecureFlag                bool
	squashedFlag                bool
	localIpFlag                 string
)

func init() {
	torrentCommand.AddCommand(torrentPullCommand)
	torrentCommand.PersistentFlags().IntVar(&torrentLowerPort, "lower-port", 6881, "Lower port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentUpperPort, "upper-port", 6889, "Upper port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentConnectionsPerSecond, "connections-per-second", 200, "Number of connection attempts that are made per second")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxDowloadRate, "download-rate", 0, "Maximum download rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxUploadRate, "upload-rate", 0, "Maximum upload rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentEncryptionMode, "encryption-mode", int(bittorrent.FORCED), "Encryption mode for connections. 0 means that only encrypted connections are allowed, 1 that encryption is preferred but not enforced and 2 that encryption is disabled.")
	torrentCommand.PersistentFlags().BoolVar(&torrentDebug, "debug", false, "BitTorrent protocol verbosity")
	torrentCommand.PersistentFlags().BoolVar(&insecureFlag, "insecure", false, "If specified, HTTP is used in place of HTTPS to talk to the registry")
	torrentCommand.PersistentFlags().BoolVar(&squashedFlag, "squashed", false, "If specified, the squashed version of the image will be pulled")
	torrentCommand.PersistentFlags().StringVar(&localIpFlag, "local-ip", "localhost", "The IP address of the local machine. Used to connect Docker to quayctl.")

	torrentCommand.AddCommand(torrentSeedCommand)
	torrentSeedCommand.Flags().DurationVar(&torrentSeedDuration, "duration", 0, "Duration of the seeding. If not specified, will seed forever.")

	torrentFolder = os.TempDir() + "/quayctl/torrents"
	torrentFingerprint = bittorrent.ClientFingerprint{"QU", 0, 1, 0, 0}
}

var torrentCommand = &cobra.Command{
	Use:   "torrent",
	Short: "interact with Quay via BitTorrent",
	Run:   torrentAction,
}

var torrentSeedCommand = &cobra.Command{
	Use:   "seed",
	Short: "upload a container image indefinitely",
	Run:   torrentSeedRun,
}

func torrentAction(cmd *cobra.Command, args []string) {
	cmd.Usage()
	os.Exit(1)
}

var torrentPullCommand = &cobra.Command{
	Use:   "pull",
	Short: "pull a container image",
	Run:   torrentPullRun,
}

func torrentPullRun(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be pulled")
	}

	image := args[0]

	if squashedFlag {
		if err := torrentSquashedImage(image, dockerPerformLoad, torrentNoSeed); err != nil {
			log.Fatal(err)
		}

		log.Printf("Successfully pulled squashed image %v", image)
	} else {
		if err := torrentImage(image, dockerPerformLoad, dockerSkipExistingLayers, torrentNoSeed, localIpFlag); err != nil {
			log.Fatal(err)
		}

		log.Printf("Successfully pulled image %v", image)
	}
}

func torrentSeedRun(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be seeded")
	}

	image := args[0]

	if squashedFlag {
		if err := torrentSquashedImage(image, dockerSkipLoad, torrentSeedAfterPull); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := torrentImage(image, dockerSkipLoad, dockerAllLayers, torrentSeedAfterPull, localIpFlag); err != nil {
			log.Fatal(err)
		}
	}
}
