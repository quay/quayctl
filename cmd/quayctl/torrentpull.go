package main

import (
	"log"

	"github.com/spf13/cobra"
)

var torrentPullCommand = &cobra.Command{
	Use:   "torrent",
	Short: "Pull an image via BitTorrent",
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
