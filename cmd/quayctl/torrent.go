package main

import (
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/coreos-inc/testpull/bittorrent"
	"github.com/coreos-inc/testpull/manifest"
)

var torrentFolder string
var torrentPort int
var seedDuration time.Duration

func init() {
	torrentCommand.AddCommand(torrentPullCommand)
	torrentCommand.PersistentFlags().IntVar(&torrentPort, "port", 0, "Port that listens for peer connections")

	torrentCommand.AddCommand(torrentSeedCommand)
	torrentSeedCommand.Flags().DurationVar(&seedDuration, "duration", 10*time.Minute, "Duration of the seeding")

	torrentFolder = os.TempDir() + "/quayctl/torrents"
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

	// Download the image manifest.
	retrieved, err := manifest.Download(image)
	if err != nil {
		log.Fatalf("Could not download image: %v", err)
	}

	log.Printf("Got manifest: %v", retrieved)

	// TODO(jschorr): implement this
	torrents := []string{}

	// TODO(jzelinskie): Mute logs because Taipei-Torrent is super-verbose.
	// log.SetFlags(0)
	// log.SetOutput(ioutil.Discard)

	// Initialize Bittorrent client.
	bt := initBitTorrentClient(torrentPort)
	defer bt.Shutdown()

	// Download every layers in parallel.
	paths := parallelTorrents(bt, torrents, 0)
	log.Printf("Downloaded every layers to %v\n", paths)

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
	if len(args) != 1 {
		log.Fatal("failed to specify one image to be seeded")
	}
	image := args[0]

	// TODO(jschorr): implement this
	torrents := []string{}

	// TODO(jzelinskie): Mute logs because Taipei-Torrent is super-verbose.
	// log.SetFlags(0)
	// log.SetOutput(ioutil.Discard)

	// Initialize Bittorrent client.
	bt := initBitTorrentClient(torrentPort)
	defer bt.Shutdown()

	// Seed every layers in parallel.
	parallelTorrents(bt, torrents, seedDuration)

	log.Println("successfully seeded image:", image)
}

func initBitTorrentClient(port int) *bittorrent.Client {
	// Ensure destination folder exists.
	if err := os.MkdirAll(torrentFolder, 0755); err != nil {
		log.Fatal(err)
	}

	bt, err := bittorrent.NewClient(port, torrentFolder)
	if err != nil {
		log.Fatal(err)
	}

	go bt.Run()
	time.Sleep(100 * time.Millisecond)

	return bt
}

func parallelTorrents(bt *bittorrent.Client, torrents []string, seedDuration time.Duration) (paths []string) {
	ch := make(chan string)

	for _, torrent := range torrents {
		go func(torrent string) {
			// Download and wait for download to finish.
			log.Printf("Downloading %s\n", torrent)
			path, keepSeeding, err := bt.Download(torrent, seedDuration)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Finished downloading %s\n", torrent)

			// Wait for seed to finish.
			if seedDuration > 0 {
				log.Printf("Seeding %s for %v\n", torrent, seedDuration)
				<-keepSeeding
				log.Printf("Stopped seeding %v\n", torrent)
			}

			// Signal success.
			ch <- path
		}(torrent)
	}

	// Wait for every torrents to finish.
	for range torrents {
		select {
		case path := <-ch:
			paths = append(paths, path)
		}
	}

	return
}
