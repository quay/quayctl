package main

import (
	"log"

	"github.com/coreos-inc/testpull/manifest"

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

	// Download the image manifest.
	retrieved, err := manifest.Download(image)
	if err != nil {
		log.Fatalf("Could not download image: %v", err)
	}

	log.Printf("Got manifest: %v", retrieved)

	// TODO(quentin-m): implement this
	// layersPath, err := DownloadLayers(manifest)
	// if err != nil {
	//   return err
	// }

	// TODO(jschorr): implement this
	// err = ImportImage(imagePath)
	// if err != nil {
	//   return err
	// }

	log.Println("successfully imported image:", image)
}
