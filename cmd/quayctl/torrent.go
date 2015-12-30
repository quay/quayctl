package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	torrentCommand.AddCommand(torrentPullCommand)
	torrentCommand.AddCommand(torrentSeedCommand)
}

var torrentCommand = &cobra.Command{
	Use:   "torrent",
	Short: "interact with Quay via BitTorrent",
	Run:   torrentAction,
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

	// TODO(jschorr): implement this
	// manifest, err := DownloadManifest(image)
	// if err != nil {
	//   return err
	// }

	// TODO(quentin-m): implement this
	// imagePath, err := DownloadImage(image)
	// if err != nil {
	//   return err
	// }

	// TODO(jschorr): implement this
	// err = ImportImage(manifest, imagePath)
	// if err != nil {
	//   return err
	// }

	log.Println("successfully imported image:", image)
}

var torrentSeedCommand = &cobra.Command{
	Use:   "seed",
	Short: "upload a container image indefinitely",
	Run:   torrentSeedRun,
}

func torrentSeedRun(cmd *cobra.Command, args []string) {
	// TODO(quentin-m): implement this
}
