package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
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
	blobSum     string
	torrentPath string
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

// buildTorrentInfo builds the slice of torrentInfo structs representing each blob sum to be
// downloaded, along with its torrent URL.
func buildTorrentInfo(named reference.Named, blobs []schema1.FSLayer, credentials types.AuthConfig) []torrentInfo {
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

		torrents = append(torrents, torrentInfo{blobSum, torrentURL.String()})
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
	torrents := buildTorrentInfo(named, blobs, credentials)

	// Initialize Bittorrent client.
	bt, err := initBitTorrentClient()
	if err != nil {
		return fmt.Errorf("Could not initialize torrent client: %v", err)
	}

	defer bt.Stop()

	// Add a channel for each layer and blob to conduct post-processing.
	layerCompletedChannels := map[string]chan struct{}{}
	blobDownloadedChannels := map[string]chan struct{}{}
	blobCompletedChannels := map[string]chan struct{}{}
	blobPaths := cmap.New()

	// Create the blob channels.
	for _, torrent := range torrents {
		blobDownloadedChannels[torrent.blobSum] = make(chan struct{})
		blobCompletedChannels[torrent.blobSum] = make(chan struct{})
	}

	// Create the layer channels.
	for _, layer := range layers {
		layerCompletedChannels[layer.info.ID] = make(chan struct{})
	}

	// Create a progress bar for each of the blobs.
	pbMap := map[string]*pb.ProgressBar{}
	var bars = make([]*pb.ProgressBar, 0)
	for _, torrent := range torrents {
		progressBar := pb.New(100).Prefix(torrent.blobSum + ": ").Postfix(": Initializing")
		progressBar.AlwaysUpdate = true

		pbMap[torrent.blobSum] = progressBar
		bars = append(bars, progressBar)
	}

	pool, err := pb.StartPool(bars...)
	if err != nil {
		panic(err)
	}

	// Start goroutines to conduct the layer work.
	if loadOption == dockerPerformLoad {
		for _, layer := range layers {
			go func(layer layerInfo) {
				// Wait on the layer's blob to be downloaded.
				blobSum := manifest.FSLayers[layer.index].BlobSum.String()
				<-blobDownloadedChannels[blobSum]

				// Wait on the layer's parent (if any) to be loaded.
				if layer.parentInfo != nil {
					<-layerCompletedChannels[layer.parentInfo.ID]
				}

				// Call docker-load on the layer.
				layerPath, _ := blobPaths.Get(blobSum)
				err := dockerclient.DockerLoadLayer(named, manifest, layer.index, layerPath.(string))
				if err != nil {
					log.Fatal(err)
				}

				// Mark the layer as completed.
				close(layerCompletedChannels[layer.info.ID])
			}(layer)
		}
	}

	// For each torrent, download the layers in parallel, call post-processing and (optionally)
	// seed.
	var localSeedDuration *time.Duration = nil
	if seedOption == torrentSeedAfterPull {
		localSeedDuration = &torrentSeedDuration
	}

	for _, torrent := range torrents {
		go func(torrent torrentInfo) {
			// Add a goroutine to update the progessbar for the torrent.
			go func(torrent torrentInfo) {
				progressBar := pbMap[torrent.blobSum]

				for {
					select {
					case <-blobCompletedChannels[torrent.blobSum]:
						progressBar.Postfix(": Complete").Set(100)
						return

					case <-time.After(250 * time.Millisecond):
						status, err := bt.GetStatus(torrent.torrentPath)
						if err == nil {
							progressBar.Set(int(status.Progress))
							progressBar.Postfix(fmt.Sprintf(": %s %v/s ▼ %v/s ▲", status.Status, humanize.Bytes(uint64(status.DownloadRate*1024)), humanize.Bytes(uint64(status.UploadRate*1024))))
						}

						break
					}
				}
			}(torrent)

			// Start downloading the torrent.
			path, keepSeeding, err := bt.Download(torrent.torrentPath, torrentFolder, localSeedDuration)
			if err != nil {
				log.Fatal(err)
			}

			blobPaths.Set(torrent.blobSum, path)

			// Mark the download as complete.
			close(blobDownloadedChannels[torrent.blobSum])

			// Wait for seed to finish.
			if localSeedDuration != nil {
				<-keepSeeding
			}

			// Signal success.
			close(blobCompletedChannels[torrent.blobSum])
		}(torrent)
	}

	// Wait for every torrent and every layer to finish.
	for _, torrent := range torrents {
		<-blobCompletedChannels[torrent.blobSum]
	}

	pool.Stop()

	if loadOption == dockerPerformLoad {
		for _, layer := range layers {
			log.Println("Importing layer", layer.info.ID)
			<-layerCompletedChannels[layer.info.ID]
		}
	}

	return nil
}

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
