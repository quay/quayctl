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
	torrentSeedCommand.Flags().DurationVar(&torrentSeedDuration, "duration", 10*time.Minute, "Duration of the seeding")

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
		torrentURL := url.URL{
			Scheme: "https",
			Host:   named.Hostname(),
			Path:   fmt.Sprintf("/c1/torrent/%s/blobs/%s", named.RemoteName(), layer.BlobSum.String()),
		}

		if insecureFlag {
			torrentURL.Scheme = "http"
		}

		if credentials.Username != "" {
			torrentURL.User = url.UserPassword(credentials.Username, credentials.Password)
		}

		if _, found := blobSet[layer.BlobSum.String()]; found {
			continue
		}

		torrents = append(torrents, torrentInfo{layer.BlobSum.String(), torrentURL.String()})
		blobSet[layer.BlobSum.String()] = struct{}{}
	}

	// Initialize Bittorrent client.
	bt := initBitTorrentClient()
	defer bt.Stop()

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

	// TODO(jschorr): implement this

	// Initialize Bittorrent client.
	// bt := initBitTorrentClient()
	// defer bt.Stop()

	// Seed every layers in parallel.
	// parallelTorrents(bt, torrents, torrentSeedDuration)

	log.Println("successfully seeded image:", image)
}

func initBitTorrentClient() *bittorrent.Client {
	// Ensure destination folder exists.
	if err := os.MkdirAll(torrentFolder, 0755); err != nil {
		log.Fatal(err)
	}

	// Create client.
	bt := bittorrent.NewClient(bittorrent.ClientConfig{
		LowerListenPort:      torrentLowerPort,
		UpperListenPort:      torrentUpperPort,
		ConnectionsPerSecond: torrentConnectionsPerSecond,
		MaxDownloadRate:      torrentMaxDowloadRate * 1024,
		MaxUploadRate:        torrentMaxUploadRate * 1024,
		Encryption:           bittorrent.EncryptionMode(torrentEncryptionMode),
		Debug:                torrentDebug,
	})

	// Start client.
	if err := bt.Start(); err != nil {
		log.Fatal(err)
	}

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
			path, keepSeeding, err := bt.Download(torrent.torrentPath, torrentFolder, seedDuration)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Finished downloading %s to %s\n", torrent.torrentPath, path)

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
		path := <-ch
		results[path.key] = path.torrentPath
	}

	return results
}
