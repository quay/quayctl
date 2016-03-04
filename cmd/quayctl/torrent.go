// Copyright 2015 CoreOS, Inc.
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
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"
	"github.com/dustin/go-humanize"
	"github.com/streamrail/concurrent-map"

	"github.com/coreos/quayctl/bittorrent"
	"github.com/coreos/quayctl/dockerclient"
	"github.com/coreos/quayctl/dockerdist"
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
		var parentInfo *dockerclient.V1LayerInfo

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
	for index := range manifest.History {
		found, _ := dockerclient.HasImage(manifest.FSLayers[index].BlobSum.String())
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
func torrentImage(image string, loadOption dockerLoadOption, layersOption dockerLayersOption, seedOption torrentSeedOption, localIp string) error {
	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Retrieve the manifest for the image.
	named, manifest, err := dockerdist.DownloadManifest(image, insecureFlag)
	if err != nil {
		return fmt.Errorf("Could not download image manifest: %v", err)
	}

	// Ensure that the manifest type is supported.
	switch manifest.(type) {
	case *schema1.SignedManifest:
		break
	default:
		return errors.New("only v1 manifests are currently supported")
	}
	v1Manifest := manifest.(*schema1.SignedManifest)

	log.Printf("Downloaded manifest for image %v", image)

	// Build the lists of layers and blobs that we need to download.
	layers, blobs := requiredLayersAndBlobs(v1Manifest, layersOption)
	if layersOption == dockerSkipExistingLayers && len(layers) == 0 && seedOption == torrentNoSeed {
		log.Printf("All layers already downloaded")
		return nil
	}

	// Build the list of torrent URLs, one per file system layer needed for download.
	torrents := buildTorrentInfoForBlob(named, blobs, credentials)
	downloadInfo := downloadTorrents(torrents, seedOption)

	if loadOption == dockerPerformLoad {
		// Wait for all layers to be downloaded.
		blobPaths := map[string]string{}
		for _, layer := range layers {
			blobSum := v1Manifest.FSLayers[layer.index].BlobSum.String()
			<-downloadInfo.downloadedChannels[blobSum]
			blobPath, _ := downloadInfo.torrentPaths.Get(blobSum)
			blobPaths[blobSum] = blobPath.(string)
		}

		if downloadInfo.hasProgressBars {
			downloadInfo.pool.Stop()
		}

		// Perform the docker load.
		lerr := dockerclient.DockerLoad(named, v1Manifest, blobPaths, localIp)
		if lerr != nil {
			log.Fatalf("%v", lerr)
		}
	}

	// Wait until all torrents are complete.
	<-downloadInfo.completeChannel

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
	log.Println("Starting download of squashed image")
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
	hasProgressBars    bool                     // Whether progress bars are running.
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
		progressBar := pb.New(100).Prefix(shortenName(torrent.title)).Postfix(" Initializing")
		progressBar.SetMaxWidth(80)
		progressBar.ShowCounters = false
		progressBar.AlwaysUpdate = true

		pbMap[torrent.id] = progressBar
		bars = append(bars, progressBar)
	}

	// Create a pool of progress bars.
	pool, err := pb.StartPool(bars...)
	var hasProgressBars = true
	if err != nil {
		hasProgressBars = false
	}

	if torrentDebug {
		pool.Stop()
		hasProgressBars = false
	}

	// Initialize Bittorrent client.
	bt, err := initBitTorrentClient()
	if err != nil {
		panic(fmt.Errorf("Could not initialize torrent client: %v", err))
	}

	// Listen for Ctrl-C.
	go catchShutdownSignals(bt, pool, hasProgressBars)

	// For each torrent, download the data in parallel, call post-processing and (optionally)
	// seed.
	var localSeedDuration *time.Duration
	if seedOption == torrentSeedAfterPull {
		localSeedDuration = &torrentSeedDuration
	}

	// Create the completed channel.
	completed := make(chan struct{})

	// Start a goroutine to query the torrent system for its status. Since libtorrent is single
	// threaded via cgo, we need this to be done in a central source.
	// Add a goroutine to update the progessbar for the torrent.
	if hasProgressBars {
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
							progressBar.Postfix(fmt.Sprintf(" %s DL%v/s UL%v/s", status.Status, humanize.Bytes(uint64(status.DownloadRate*1024)), humanize.Bytes(uint64(status.UploadRate*1024))))
						}
					}
				}
			}
		}()
	} else {
		// Write the status every 30s for each torrent.
		go func() {
			for {
				select {
				case <-completed:
					return

				case <-time.After(30 * time.Second):
					for _, torrent := range torrents {
						status, err := bt.GetStatus(torrent.torrentPath)
						if err == nil {
							log.Printf("Torrent %v: %s DL%v/s UL%v/s", shortenName(torrent.title), status.Status, humanize.Bytes(uint64(status.DownloadRate*1024)), humanize.Bytes(uint64(status.UploadRate*1024)))
						}
					}
				}
			}
		}()
	}

	// Start the downloads for each torrent.
	for _, torrent := range torrents {
		go func(torrent torrentInfo) {
			// Start downloading the torrent.
			path, keepSeeding, err := bt.Download(torrent.torrentPath, torrentFolder, localSeedDuration)
			if err != nil {
				if hasProgressBars {
					pool.Stop()
				}

				log.Fatal(err)
			}

			torrentPaths.Set(torrent.id, path)

			if hasProgressBars {
				pbMap[torrent.id].ShowBar = false
				pbMap[torrent.id].ShowPercent = false
				pbMap[torrent.id].ShowTimeLeft = false
				pbMap[torrent.id].ShowSpeed = false
				pbMap[torrent.id].Postfix(" Completed").Set(100)
			} else {
				log.Printf("Completed download of layer %v\n", torrent.id)
			}

			// Mark the download as complete.
			close(torrentDownloadedChannels[torrent.id])

			// Wait for seed to finish.
			if localSeedDuration != nil {
				if !hasProgressBars {
					log.Printf("Seeding layer %v\n", torrent.id)
				}
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

		if hasProgressBars {
			pool.Stop()
		}

		bt.Stop()
		close(completed)
	}()

	return downloadTorrentInfo{torrentDownloadedChannels, completed, pool, hasProgressBars, torrentPaths}
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

func catchShutdownSignals(btClient *bittorrent.Client, progressBars *pb.Pool, hasProgressBars bool) {
	shutdown := make(chan os.Signal)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	<-shutdown

	if hasProgressBars {
		progressBars.Stop()
	}

	btClient.Stop()

	log.Println("Received signal and cleanly shutdown.")
	os.Exit(0)
}

func shortenName(name string) string {
	if len(name) > 19 {
		return name[:19]
	}
	return name
}
