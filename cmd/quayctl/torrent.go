package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/reference"
	"github.com/dustin/go-humanize"
	"github.com/streamrail/concurrent-map"

	"github.com/coreos-inc/quayctl/bittorrent"
	"github.com/coreos-inc/quayctl/dockerclient"
	"github.com/coreos-inc/quayctl/dockerdist"
)

// torrentSeedOption defines the option for whether to seed after a layer has been downloaded
// via torrent.
type torrentSeedOption int

const (
	torrentNoSeed torrentSeedOption = iota
	torrentSeedAfterPull
)

// dockerLoadOption defines the option for whether to perform docker-load of a downloaded layer.
type dockerLoadOption int

const (
	dockerSkipLoad dockerLoadOption = iota
	dockerPerformLoad
)

// dockerLayersOption defines the option for whether to check for the existance of layers in
// Docker and to skip those found.
type dockerLayersOption int

const (
	dockerSkipExistingLayers dockerLayersOption = iota
	dockerAllLayers
)

// torrentInfo holds the blobSum and torrent path for a torrent.
type torrentInfo struct {
	id          string
	torrentPath string
	title       string
}

// layerInfo holds information about a Docker layer in an image.
type layerInfo struct {
	info       dockerclient.V1LayerInfo
	layer      schema1.History
	index      int
	parentInfo *dockerclient.V1LayerInfo
}

func buildLayerInfo(layers []schema1.History) []layerInfo {
	info := make([]layerInfo, len(layers))
	for index, layer := range layers {
		parentIndex := index + 1
		var parentInfo *dockerclient.V1LayerInfo = nil

		if parentIndex < len(layers) {
			parentInfoStruct := dockerclient.GetLayerInfo(layers[parentIndex])
			parentInfo = &parentInfoStruct
		}

		info[index] = layerInfo{dockerclient.GetLayerInfo(layer), layer, index, parentInfo}
	}
	return info
}

// requiredLayersAndBlobs returns the list of required layers and blobs that we need to download.
func requiredLayersAndBlobs(manifest *schema1.SignedManifest, option dockerLayersOption) ([]layerInfo, []schema1.FSLayer) {
	if option == dockerAllLayers {
		return buildLayerInfo(manifest.History), manifest.FSLayers
	}

	// Check each layer for its existance in Docker.
	var blobsToDownload = make([]schema1.FSLayer, 0)
	for index, layer := range manifest.History {
		layerInfo := dockerclient.GetLayerInfo(layer)
		found, _ := dockerclient.HasImage(layerInfo.ID)
		if found {
			return buildLayerInfo(manifest.History[0:index]), blobsToDownload
		}

		blobsToDownload = append(blobsToDownload, manifest.FSLayers[index])
	}

	return buildLayerInfo(manifest.History), manifest.FSLayers
}

// buildTorrentInfoForBlob builds the slice of torrentInfo structs representing each blob sum to be
// downloaded, along with its torrent URL.
func buildTorrentInfoForBlob(named reference.Named, blobs []schema1.FSLayer, credentials types.AuthConfig) []torrentInfo {
	blobSet := map[string]struct{}{}

	var torrents = make([]torrentInfo, 0)
	for _, blob := range blobs {
		blobSum := blob.BlobSum.String()
		torrentURL := url.URL{
			Scheme: "https",
			Host:   named.Hostname(),
			Path:   fmt.Sprintf("/c1/torrent/%s/blobs/%s", named.RemoteName(), blobSum),
		}

		if insecureFlag {
			torrentURL.Scheme = "http"
		}

		if credentials.Username != "" {
			torrentURL.User = url.UserPassword(credentials.Username, credentials.Password)
		}

		if _, found := blobSet[blobSum]; found {
			continue
		}

		torrents = append(torrents, torrentInfo{blobSum, torrentURL.String(), blobSum})
		blobSet[blobSum] = struct{}{}
	}

	return torrents
}

// torrentImage performs a torrent download of a Docker image, with specified options for loading,
// cache checking and seeding.
func torrentImage(image string, loadOption dockerLoadOption, layersOption dockerLayersOption, seedOption torrentSeedOption) error {
	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Retrieve the manifest for the image.
	named, manifest, err := dockerdist.DownloadManifest(image, insecureFlag)
	if err != nil {
		return fmt.Errorf("Could not download image manifest: %v", err)
	}

	log.Printf("Downloaded manifest for image %v", image)

	// Build the lists of layers and blobs that we need to download.
	layers, blobs := requiredLayersAndBlobs(manifest, layersOption)
	if layersOption == dockerSkipExistingLayers && len(layers) == 0 && seedOption == torrentNoSeed {
		log.Printf("All layers already downloaded")
		return nil
	}

	// Build the list of torrent URLs, one per file system layer needed for download.
	torrents := buildTorrentInfoForBlob(named, blobs, credentials)
	downloadInfo := downloadTorrents(torrents, seedOption)

	// Start goroutines to conduct the layer loading work.
	layerCompletedChannels := map[string]chan struct{}{}
	if loadOption == dockerPerformLoad {
		// Create the layer channels.
		for _, layer := range layers {
			layerCompletedChannels[layer.info.ID] = make(chan struct{})
		}

		for _, layer := range layers {
			go func(layer layerInfo) {
				// Wait on the layer's blob to be downloaded.
				blobSum := manifest.FSLayers[layer.index].BlobSum.String()
				<-downloadInfo.downloadedChannels[blobSum]

				// Wait on the layer's parent (if any) to be loaded.
				if layer.parentInfo != nil {
					<-layerCompletedChannels[layer.parentInfo.ID]
				}

				// Call docker-load on the layer.
				layerPath, _ := downloadInfo.torrentPaths.Get(blobSum)
				err := dockerclient.DockerLoadLayer(named, manifest, layer.index, layerPath.(string))
				if err != nil {
					downloadInfo.pool.Stop()
					log.Fatal(err)
				}

				// Mark the layer as completed.
				close(layerCompletedChannels[layer.info.ID])
			}(layer)
		}
	}

	// Wait until all torrents are complete.
	<-downloadInfo.completeChannel

	// Ensure all layers are imported.
	if loadOption == dockerPerformLoad {
		for index, _ := range layers {
			layer := layers[len(layers)-index-1]
			log.Println("Importing layer", layer.info.ID)
			<-layerCompletedChannels[layer.info.ID]
		}
	}

	return nil
}

// torrentSquashedImage performs a torrent download of a squashed Docker image, with specified
// options for loading and seeding.
func torrentSquashedImage(image string, loadOption dockerLoadOption, seedOption torrentSeedOption) error {
	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Parse the name of the image.
	named, err := reference.ParseNamed(image)
	if err != nil {
		return err
	}

	var tagName = "latest"
	if tagged, ok := named.(reference.NamedTagged); ok {
		tagName = tagged.Tag()
	}

	// Build the URL for the squashed image.
	squashedURL := url.URL{
		Scheme: "https",
		Host:   named.Hostname(),
		Path:   fmt.Sprintf("/c1/squash/%s/%s", named.RemoteName(), tagName),
	}

	if insecureFlag {
		squashedURL.Scheme = "http"
	}

	if credentials.Username != "" {
		squashedURL.User = url.UserPassword(credentials.Username, credentials.Password)
	}

	torrent := torrentInfo{
		id:          "squashed",
		torrentPath: squashedURL.String(),
		title:       fmt.Sprintf("%s/%s:%s.squash", named.Hostname(), named.RemoteName(), tagName),
	}

	// Start the download of the torrent.
	downloadInfo := downloadTorrents([]torrentInfo{torrent}, seedOption)

	// Wait for the torrent to complete.
	<-downloadInfo.completeChannel

	// Call docker-load on the squashed image.
	path, _ := downloadInfo.torrentPaths.Get("squashed")
	squashedFile, err := os.Open(path.(string))
	if err != nil {
		log.Fatal(err)
	}

	defer squashedFile.Close()

	log.Println("Importing squashed image")
	return dockerclient.DockerLoadTar(squashedFile)
}

// downloadTorrentInfo contains data structures populated and signaled by the downloadTorrents
// method.
type downloadTorrentInfo struct {
	downloadedChannels map[string]chan struct{} // Map of torrent ID -> channel to await download
	completeChannel    chan struct{}            // Channel to await completion of all torrent ops
	pool               *pb.Pool                 // ProgressBar pool
	torrentPaths       cmap.ConcurrentMap       // Map from torrent ID -> downloaded path
}

// downloadTorrents starts the downloads of all the specified torrents, with optional seeding once
// completed. Returns immediately with a downloadTorrentInfo struct.
func downloadTorrents(torrents []torrentInfo, seedOption torrentSeedOption) downloadTorrentInfo {
	// Add a channel for each torrent to track state.
	torrentDownloadedChannels := map[string]chan struct{}{}
	torrentCompletedChannels := map[string]chan struct{}{}
	torrentPaths := cmap.New()

	// Create the torrent channels.
	for _, torrent := range torrents {
		torrentDownloadedChannels[torrent.id] = make(chan struct{})
		torrentCompletedChannels[torrent.id] = make(chan struct{})
	}

	// Create a progress bar for each of the torrents.
	pbMap := map[string]*pb.ProgressBar{}
	var bars = make([]*pb.ProgressBar, 0)
	for _, torrent := range torrents {
		progressBar := pb.New(100).Prefix(torrent.title + ": ").Postfix(": Initializing")
		progressBar.AlwaysUpdate = true

		pbMap[torrent.id] = progressBar
		bars = append(bars, progressBar)
	}

	// Create a pool of progress bars.
	pool, err := pb.StartPool(bars...)
	if err != nil {
		panic(err)
	}

	// Initialize Bittorrent client.
	bt, err := initBitTorrentClient()
	if err != nil {
		panic(fmt.Errorf("Could not initialize torrent client: %v", err))
	}

	// Listen for Ctrl-C.
	go catchShutdownSignals(bt, pool)

	// For each torrent, download the data in parallel, call post-processing and (optionally)
	// seed.
	var localSeedDuration *time.Duration = nil
	if seedOption == torrentSeedAfterPull {
		localSeedDuration = &torrentSeedDuration
	}

	// Start a goroutine to query the torrent system for its status. Since libtorrent is single
	// threaded via cgo, we need this to be done in a central source.
	// Add a goroutine to update the progessbar for the torrent.

	// Create the completed channel.
	completed := make(chan struct{})

	go func() {
		for {
			select {
			case <-completed:
				return

			case <-time.After(250 * time.Millisecond):
				for _, torrent := range torrents {
					progressBar := pbMap[torrent.id]
					status, err := bt.GetStatus(torrent.torrentPath)
					if err == nil {
						progressBar.Set(int(status.Progress))
						progressBar.Postfix(fmt.Sprintf(": %s %v/s ▼ %v/s ▲", status.Status, humanize.Bytes(uint64(status.DownloadRate*1024)), humanize.Bytes(uint64(status.UploadRate*1024))))
					}
				}
			}
		}
	}()

	// Start the downloads for each torrent.
	for _, torrent := range torrents {
		go func(torrent torrentInfo) {
			// Start downloading the torrent.
			path, keepSeeding, err := bt.Download(torrent.torrentPath, torrentFolder, localSeedDuration)
			if err != nil {
				pool.Stop()
				log.Fatal(err)
			}

			torrentPaths.Set(torrent.id, path)

			// Mark the download as complete.
			close(torrentDownloadedChannels[torrent.id])
			pbMap[torrent.id].Set(100)
			pbMap[torrent.id].Postfix(": Completed")

			// Wait for seed to finish.
			if localSeedDuration != nil {
				<-keepSeeding
			}

			// Signal success.
			close(torrentCompletedChannels[torrent.id])
		}(torrent)
	}

	// Start a goroutine to wait for all torrents to complete.
	go func() {
		// Wait for every torrent to finish.
		for _, torrent := range torrents {
			<-torrentCompletedChannels[torrent.id]
		}

		pool.Stop()
		bt.Stop()
		close(completed)
	}()

	return downloadTorrentInfo{torrentDownloadedChannels, completed, pool, torrentPaths}
}

// initBitTorrentClient inityializes a bittorrent client.
func initBitTorrentClient() (*bittorrent.Client, error) {
	// Ensure destination folder exists.
	if err := os.MkdirAll(torrentFolder, 0755); err != nil {
		return nil, err
	}

	// Create client.
	bt := bittorrent.NewClient(bittorrent.ClientConfig{
		Fingerprint:          torrentFingerprint,
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
		return nil, err
	}

	return bt, nil
}

func catchShutdownSignals(btClient *bittorrent.Client, progressBars *pb.Pool) {
	shutdown := make(chan os.Signal)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown

	progressBars.Stop()
	btClient.Stop()

	log.Println("Received signal and cleanly shutdown.")
	os.Exit(0)
}
