// dockerclient package provides helper methods for creating a synthesized docker load TAR stream
// and loading it into the local Docker daemon.
package dockerclient

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/reference"
	"github.com/fsouza/go-dockerclient"
)

// DockerLoadLayer performs a `docker load` of a single layer found at the given index in the
// manifest. Note that calling this method is sensitive to the dependent layers being already loaded
// in Docker, otherwise it will fail.
func DockerLoadLayer(image reference.Named, manifest *schema1.SignedManifest, layerIndex int, layerPath string) error {
	client, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("Could not connect to Docker: %v", err)
	}

	buf := new(bytes.Buffer)
	opts := docker.LoadImageOptions{buf}

	terr := buildDockerLoadLayerTar(image, manifest, layerIndex, layerPath, buf)
	if terr != nil {
		return fmt.Errorf("Could not perform docker-load: %v", terr)
	}

	lerr := client.LoadImage(opts)
	if lerr != nil {
		return fmt.Errorf("Could not perform docker-load: %v", lerr)
	}

	return nil
}

type V1LayerInfo struct {
	ID string `json:"id"`
}

// GetLayerInfo returns the parsed V1 layer information for the given layer.
func GetLayerInfo(layerHistory schema1.History) V1LayerInfo {
	layerInfo := V1LayerInfo{}
	err := json.Unmarshal([]byte(layerHistory.V1Compatibility), &layerInfo)
	if err != nil {
		log.Fatalf("Could not unmarshal V1 compatibility information")
	}

	return layerInfo
}

// buildDockerLoadLayerTar builds a TAR in the format of `docker load` V1 for a single layer.
func buildDockerLoadLayerTar(image reference.Named, manifest *schema1.SignedManifest, layerIndex int, layerPath string, w io.Writer) error {
	// Docker import V1 Format (.tar):
	//  VERSION - The docker import version: '1.0'
	//  repositories - JSON file containing a repo -> tag -> image map
	//  {image ID folder}:
	//    json - The layer JSON
	//     layer.tar - The TARed contents of the layer

	tw := tar.NewWriter(w)

	// Write the VERSION file.
	writeTarFile(tw, "VERSION", []byte("1.0"))

	// Write the repositories file
	//
	// {
	//   "quay.io/some/repo": {
	//      "latest": "finallayerid"
	//   }
	// }
	repositoriesMap := map[string]interface{}{}

	// Note: We only write the tagged information for the top layer.
	if layerIndex == 0 {
		topLayerId := GetLayerInfo(manifest.History[0]).ID
		tagMap := map[string]string{}
		tagMap[manifest.Tag] = topLayerId
		repositoriesMap[image.Hostname()+"/"+image.RemoteName()] = tagMap
	}

	jsonString, _ := json.Marshal(repositoriesMap)
	writeTarFile(tw, "repositories", []byte(jsonString))

	// Write a folder containing its JSON information, as well as its layer TAR.
	layerInfo := GetLayerInfo(manifest.History[layerIndex])

	// {someLayerId}/json
	writeTarFile(tw, fmt.Sprintf("%s/json", layerInfo.ID), []byte(manifest.History[layerIndex].V1Compatibility))

	// {someLayerId}/layer.tar
	layerFile, err := os.Open(layerPath)
	if err != nil {
		return err
	}

	layerStat, err := layerFile.Stat()
	if err != nil {
		return err
	}

	writeTarHeader(tw, fmt.Sprintf("%s/layer.tar", layerInfo.ID), layerStat.Size())
	io.Copy(tw, layerFile)
	layerFile.Close()

	// Close writing to the TAR.
	return tw.Close()
}

func writeTarHeader(tw *tar.Writer, filename string, filesize int64) {
	hdr := &tar.Header{
		Name: filename,
		Mode: 0600,
		Size: filesize,
	}

	if err := tw.WriteHeader(hdr); err != nil {
		log.Fatalln(err)
	}
}

func writeTarFile(tw *tar.Writer, filename string, data []byte) {
	writeTarHeader(tw, filename, int64(len(data)))

	if _, err := tw.Write(data); err != nil {
		log.Fatalln(err)
	}
}
