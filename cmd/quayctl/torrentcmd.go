package main

import (
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/coreos-inc/quayctl/bittorrent"
)

var torrentFingerprint bittorrent.ClientFingerprint
var torrentFolder string
var torrentLowerPort int
var torrentUpperPort int
var torrentConnectionsPerSecond int
var torrentMaxDowloadRate int
var torrentMaxUploadRate int
var torrentSeedDuration time.Duration
var torrentEncryptionMode int
var torrentDebug bool
var insecureFlag bool

func init() {
	torrentCommand.AddCommand(torrentPullCommand)
	torrentCommand.PersistentFlags().IntVar(&torrentLowerPort, "lower-port", 6881, "Lower port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentUpperPort, "upper-port", 6889, "Upper port that listens for peer connections")
	torrentCommand.PersistentFlags().IntVar(&torrentConnectionsPerSecond, "connections-per-second", 200, "Number of connection attempts that are made per second")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxDowloadRate, "download-rate", 0, "Maximum download rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentMaxUploadRate, "upload-rate", 0, "Maximum upload rate in kB/s. 0 means unlimited.")
	torrentCommand.PersistentFlags().IntVar(&torrentEncryptionMode, "encryption-mode", int(bittorrent.FORCED), "Encryption mode for connections. 0 means that only encrypted connections are allowed, 1 that encryption is preferred but not enforced and 2 that encrytion is disabled.")
	torrentCommand.PersistentFlags().BoolVar(&torrentDebug, "debug", false, "BitTorrent protocol verbosity")
	torrentCommand.PersistentFlags().BoolVar(&insecureFlag, "insecure", false, "If specified, HTTP is used in place of HTTPS to talk to the registry")

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
	err := torrentImage(image, dockerPerformLoad, dockerSkipExistingLayers, torrentNoSeed)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Successfully pulled image %v", image)
}

func torrentSeedRun(cmd *cobra.Command, args []string) {
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be seeded")
	}

	image := args[0]
	err := torrentImage(image, dockerSkipLoad, dockerAllLayers, torrentSeedAfterPull)
	if err != nil {
		log.Fatal(err)
	}
}
