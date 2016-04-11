// Copyright 2016 CoreOS, Inc.
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

package engine

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"

	"github.com/coreos/quayctl/dockerclient"
	"github.com/coreos/quayctl/dockerdist"
	"github.com/spf13/cobra"
)

var (
	squashedFlag bool
	localIpFlag  string
)

// DockerEngine defines an engine interface for interacting with Docker.
type DockerEngine struct{}

func (de DockerEngine) Name() string {
	return "docker"
}

func (de DockerEngine) Title() string {
	return "Docker Engine"
}

func (de DockerEngine) TorrentHandler() engineTorrentHandler {
	return &dockerTorrentHandler{}
}

// dockerTorrentHandler defines an interface for pulling a Docker image via torrent.
type dockerTorrentHandler struct{}

func (dth dockerTorrentHandler) DecorateCommand(command *cobra.Command) {
	command.PersistentFlags().BoolVar(&squashedFlag, "squashed", false, "If specified, the squashed version of the image will be pulled")
	command.PersistentFlags().StringVar(&localIpFlag, "local-ip", "localhost", "The IP address of the local machine. Used to connect Docker to quayctl.")
}

func (dth dockerTorrentHandler) RetrieveTorrents(image string, insecureFlag bool, option layersOption) ([]torrentInfo, interface{}, error) {
	if squashedFlag {
		return dth.retrieveTorrentsForSquashed(image, insecureFlag)
	}

	return dth.retrieveTorrents(image, insecureFlag, option)
}

func (dth dockerTorrentHandler) LoadImage(image string, downloadInfo downloadTorrentInfo, ctx interface{}) error {
	if squashedFlag {
		return dth.loadSquashedImage(image, downloadInfo, ctx)
	}

	return dth.loadImage(image, downloadInfo, ctx)
}

func (dth dockerTorrentHandler) loadSquashedImage(image string, downloadInfo downloadTorrentInfo, ctx interface{}) error {
	// Wait for the torrent to complete.
	<-downloadInfo.CompleteChannel

	// Call docker-load on the squashed image.
	path, _ := downloadInfo.TorrentPaths.Get("squashed")
	squashedFile, err := os.Open(path.(string))
	if err != nil {
		return err
	}

	defer squashedFile.Close()

	log.Println("Importing squashed image")
	return dockerclient.DockerLoadTar(squashedFile)
}

type dockerContext struct {
	v1Manifest *schema1.SignedManifest
	layers     []layerInfo
	named      reference.Named
}

func (dth dockerTorrentHandler) loadImage(image string, downloadInfo downloadTorrentInfo, ctx interface{}) error {
	dctx := ctx.(dockerContext)

	named := dctx.named
	v1Manifest := dctx.v1Manifest
	layers := dctx.layers

	// Wait for all layers to be downloaded.
	blobPaths := map[string]string{}
	for _, layer := range layers {
		blobSum := v1Manifest.FSLayers[layer.index].BlobSum.String()
		<-downloadInfo.DownloadedChannels[blobSum]
		blobPath, _ := downloadInfo.TorrentPaths.Get(blobSum)
		blobPaths[blobSum] = blobPath.(string)
	}

	if downloadInfo.HasProgressBars {
		downloadInfo.Pool.Stop()
	}

	// Perform the docker load.
	return dockerclient.DockerLoad(named, v1Manifest, blobPaths, localIpFlag)
}

// retrieveTorrentsForSquashed returns the torrent for downloading a squashed Docker image.
func (dth dockerTorrentHandler) retrieveTorrentsForSquashed(image string, insecureFlag bool) ([]torrentInfo, interface{}, error) {
	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Parse the name of the image.
	named, err := reference.ParseNamed(image)
	if err != nil {
		return []torrentInfo{}, nil, err
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

	return []torrentInfo{torrent}, nil, nil
}

// retrieveTorrents returns the torrents for downloading a Docker image.
func (dth dockerTorrentHandler) retrieveTorrents(image string, insecureFlag bool, option layersOption) ([]torrentInfo, interface{}, error) {
	// Retrieve the credentials (if any) for the current image.
	credentials, _ := dockerdist.GetAuthCredentials(image)

	// Retrieve the manifest for the image.
	named, manifest, err := dockerdist.DownloadManifest(image, insecureFlag)
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could not download image manifest: %v", err)
	}

	// Ensure that the manifest type is supported.
	switch manifest.(type) {
	case *schema1.SignedManifest:
		break

	default:
		return []torrentInfo{}, nil, errors.New("only v1 manifests are currently supported")
	}

	v1Manifest := manifest.(*schema1.SignedManifest)
	log.Printf("Downloaded manifest for image %v", image)

	// Build the lists of layers and blobs that we need to download.
	layers, blobs := dth.requiredLayersAndBlobs(v1Manifest, option)
	if option == MissingLayers && len(layers) == 0 {
		log.Printf("All layers already downloaded")
		return []torrentInfo{}, nil, nil
	}

	// Build the list of torrent URLs, one per file system layer needed for download.
	dctx := dockerContext{v1Manifest, layers, named}
	return dth.buildTorrentInfoForBlob(named, blobs, credentials, insecureFlag), dctx, nil
}

// buildTorrentInfoForBlob builds the slice of torrentInfo structs representing each blob sum to be
// downloaded, along with its torrent URL.
func (dth dockerTorrentHandler) buildTorrentInfoForBlob(named reference.Named, blobs []schema1.FSLayer, credentials types.AuthConfig, insecureFlag bool) []torrentInfo {
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

// layerInfo holds information about a Docker layer in an image.
type layerInfo struct {
	info       dockerclient.V1LayerInfo
	layer      schema1.History
	index      int
	parentInfo *dockerclient.V1LayerInfo
}

// loadLayerInfo converts the layer schema history into layerInfo structs via the Docker client.
func (dth dockerTorrentHandler) loadLayerInfo(layers []schema1.History) []layerInfo {
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
func (dth dockerTorrentHandler) requiredLayersAndBlobs(manifest *schema1.SignedManifest, option layersOption) ([]layerInfo, []schema1.FSLayer) {
	if option == AllLayers {
		return dth.loadLayerInfo(manifest.History), manifest.FSLayers
	}

	// Check each layer for its existance in Docker.
	var blobsToDownload = make([]schema1.FSLayer, 0)
	for index := range manifest.History {
		found, _ := dockerclient.HasImage(manifest.FSLayers[index].BlobSum.String())
		if found {
			return dth.loadLayerInfo(manifest.History[0:index]), blobsToDownload
		}

		blobsToDownload = append(blobsToDownload, manifest.FSLayers[index])
	}

	return dth.loadLayerInfo(manifest.History), manifest.FSLayers
}
