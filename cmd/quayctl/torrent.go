package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/coreos-inc/testpull/bittorrent"
	"github.com/coreos-inc/testpull/dockerclient"
	"github.com/coreos-inc/testpull/dockerdist"
)

var torrentFolder string
var torrentPort int
var seedDuration time.Duration
var insecureFlag bool

func init() {
	torrentCommand.AddCommand(torrentPullCommand)
	torrentCommand.PersistentFlags().IntVar(&torrentPort, "port", 0, "Port that listens for peer connections")
	torrentCommand.PersistentFlags().BoolVar(&insecureFlag, "insecure", false, "If specified, HTTP is used in place of HTTPS to talk to the registry")

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
	named, manifest, err := dockerdist.DownloadManifest(image, insecureFlag)
	if err != nil {
		log.Fatalf("Could not download image: %v", err)
	}

	log.Printf("Got manifest for image; Downloading torrents.")

	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Build the list of torrent URLs, one per file system later.
	blobSet := map[string]struct{}{}

	var torrents = make([]torrentInfo, 0)
	for _, layer := range manifest.FSLayers {
		torrentUrl := url.URL{
			Scheme: "https",
			Host:   named.Hostname(),
			Path:   fmt.Sprintf("/c1/torrent/%s/blobs/%s", named.RemoteName(), layer.BlobSum.String()),
		}

		if insecureFlag {
			torrentUrl.Scheme = "http"
		}

		if credentials.Username != "" {
			torrentUrl.User = url.UserPassword(credentials.Username, credentials.Password)
		}

		if _, found := blobSet[layer.BlobSum.String()]; found {
			continue
		}

		torrents = append(torrents, torrentInfo{layer.BlobSum.String(), torrentUrl.String()})
		blobSet[layer.BlobSum.String()] = struct{}{}
	}

	// TODO(jzelinskie): Mute logs because Taipei-Torrent is super-verbose.
	// log.SetFlags(0)
	// log.SetOutput(ioutil.Discard)

	// Initialize Bittorrent client.
	bt := initBitTorrentClient(torrentPort)
	defer bt.Shutdown()

	// Download every layers in parallel.
	results := parallelTorrents(bt, torrents, 0)
	log.Printf("All layers downloaded; calling docker load")

	// Build a synthetic tar in docker-load format and load it into Docker.
	lerr := dockerclient.DockerLoad(named, manifest, results)
	if lerr != nil {
		log.Fatalf("%v", lerr)
	}

	log.Println("Successfully imported image: ", image)
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
	// TODO: this
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

type torrentInfo struct {
	key         string
	torrentPath string
}

func parallelTorrents(bt *bittorrent.Client, torrents []torrentInfo, seedDuration time.Duration) map[string]string {
	ch := make(chan torrentInfo)

	for _, torrent := range torrents {
		go func(torrent torrentInfo) {
			// Download and wait for download to finish.
			log.Printf("Downloading %s\n", torrent.torrentPath)
			path, keepSeeding, err := bt.Download(torrent.torrentPath, seedDuration)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Finished downloading %s\n", torrent.torrentPath)

			// Wait for seed to finish.
			if seedDuration > 0 {
				log.Printf("Seeding %s for %v\n", torrent.torrentPath, seedDuration)
				<-keepSeeding
				log.Printf("Stopped seeding %v\n", torrent.torrentPath)
			}

			// Signal success.
			ch <- torrentInfo{torrent.key, path}
		}(torrent)
	}

	// Wait for every torrent to finish.
	results := map[string]string{}

	for range torrents {
		select {
		case path := <-ch:
			results[path.key] = path.torrentPath
		}
	}

	return results
}
