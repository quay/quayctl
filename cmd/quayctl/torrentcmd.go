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
	skipWebSeed                 bool
	trackers                    []string
)

func init() {
	torrentFolder = os.TempDir() + "/quayctl/torrents"
	torrentFingerprint = bittorrent.ClientFingerprint{"QU", 0, 1, 0, 0}
}

// addTorrentCommands adds the torrent pull and seed commands to the engine command.
func addTorrentCommands(engine engine, engineCommand *cobra.Command) {
	localTorrentPullRun := func(cmd *cobra.Command, args []string) {
		torrentPullRun(cmd, args, engine)
	}

	localTorrentSeedRun := func(cmd *cobra.Command, args []string) {
		torrentSeedRun(cmd, args, engine)
	}

	// Add the torrent command and its two subcommands: pull and seed.
	torrentCommand := &cobra.Command{
		Use:   "torrent",
		Short: "interact with Quay via BitTorrent",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
			os.Exit(1)
		},
	}

	torrentPullCommand := &cobra.Command{
		Use:   "pull",
		Short: "pull a container image",
		Run:   localTorrentPullRun,
	}

	torrentSeedCommand := &cobra.Command{
		Use:   "seed",
		Short: "seed a container image",
		Run:   localTorrentSeedRun,
	}

	torrentCommand.AddCommand(torrentSeedCommand)
	torrentCommand.AddCommand(torrentPullCommand)
	engineCommand.AddCommand(torrentCommand)

	// Decorate the torrent command with any engine-specific flags.
	engine.TorrentHandler().DecorateCommand(torrentCommand)
	torrentCommand.PersistentFlags().IntVar(&torrentLowerPort, "lower-port", 6881, "Lower port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentUpperPort, "upper-port", 6889, "Upper port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentConnectionsPerSecond, "connections-per-second", 200, "Number of connection attempts that are made per second")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxDowloadRate, "download-rate", 0, "Maximum download rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxUploadRate, "upload-rate", 0, "Maximum upload rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentEncryptionMode, "encryption-mode", int(bittorrent.FORCED), "Encryption mode for connections. 0 means that only encrypted connections are allowed, 1 that encryption is preferred but not enforced and 2 that encryption is disabled.")
	torrentCommand.PersistentFlags().BoolVar(&torrentDebug, "debug", false, "BitTorrent protocol verbosity")
	torrentCommand.PersistentFlags().BoolVar(&insecureFlag, "insecure", false, "If specified, HTTP is used in place of HTTPS to talk to the registry")
	torrentCommand.PersistentFlags().BoolVar(&skipWebSeed, "skip-web-seed", false, "If true, the web seed will not be used when pulling")
	torrentCommand.PersistentFlags().StringSliceVar(&trackers, "tracker", []string{}, "If specified, will override the tracker(s) used")

	torrentSeedCommand.Flags().DurationVar(&torrentSeedDuration, "duration", 0, "Duration of the seeding. If not specified, will seed forever.")
}

func torrentPullRun(cmd *cobra.Command, args []string, engine engine) {
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be pulled")
	}

	image := args[0]
	downloadConfig := bittorrent.DownloadConfig{skipWebSeed, trackers}
	handler := engine.TorrentHandler()

	// Load the torrents for the image.
	torrents, ctx, err := handler.RetrieveTorrents(image, missingLayers)
	if err != nil {
		log.Fatal(err)
	}

	// Download the image layer(s).
	downloadInfo := downloadTorrents(torrents, torrentNoSeed, downloadConfig)

	// Load the image.
	lerr := handler.LoadImage(image, downloadInfo, ctx)
	if lerr != nil {
		log.Fatal(lerr)
	}

	log.Printf("Successfully pulled image %v", image)
}

func torrentSeedRun(cmd *cobra.Command, args []string, engine engine) {
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be seeded")
	}

	image := args[0]
	downloadConfig := bittorrent.DownloadConfig{skipWebSeed, trackers}
	handler := engine.TorrentHandler()

	// Load the torrents for the image.
	torrents, _, err := handler.RetrieveTorrents(image, allLayers)
	if err != nil {
		log.Fatal(err)
	}

	// Seed the image layer(s).
	downloadInfo := downloadTorrents(torrents, torrentSeedAfterPull, downloadConfig)

	// Wait for seeding to complete.
	<-downloadInfo.completeChannel
}
