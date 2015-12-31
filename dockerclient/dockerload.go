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

// DockerLoad performs a `docker load` of the given image with its manifest and layerPaths.
func DockerLoad(image reference.Named, manifest *schema1.SignedManifest, layerPaths map[string]string) error {
	client, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("Could not connect to Docker: %v", err)
	}

	buf := new(bytes.Buffer)
	opts := docker.LoadImageOptions{buf}

	terr := buildDockerLoadTar(image, manifest, layerPaths, buf)
	if terr != nil {
		return fmt.Errorf("Could not perform docker-load: %v", terr)
	}

	lerr := client.LoadImage(opts)
	if lerr != nil {
		return fmt.Errorf("Could not perform docker-load: %v", lerr)
	}

	return nil
}

// buildDockerLoadTar builds a TAR in the format of `docker load` V1, writing it to the specified
// writer.
func buildDockerLoadTar(image reference.Named, manifest *schema1.SignedManifest, layerPaths map[string]string, w io.Writer) error {
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
	topLayerId := getLayerInfo(manifest.History[0]).ID
	tagMap := map[string]string{}
	tagMap[manifest.Tag] = topLayerId

	repositoriesMap := map[string]interface{}{}
	repositoriesMap[image.Hostname()+"/"+image.RemoteName()] = tagMap

	jsonString, _ := json.Marshal(repositoriesMap)
	writeTarFile(tw, "repositories", []byte(jsonString))

	// For each layer in the manifest, write a folder containing its JSON information, as well
	// as its layer TAR.
	for index, layer := range manifest.FSLayers {
		layerInfo := getLayerInfo(manifest.History[index])

		// {someLayerId}/json
		writeTarFile(tw, fmt.Sprintf("%s/json", layerInfo.ID), []byte(manifest.History[index].V1Compatibility))

		// {someLayerId}/layer.tar
		layerFile, err := os.Open(layerPaths[layer.BlobSum.String()])
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
	}

	// Close writing to the TAR.
	return tw.Close()
}

type v1LayerInfo struct {
	ID string `json:"id"`
}

func getLayerInfo(layerHistory schema1.History) v1LayerInfo {
	layerInfo := v1LayerInfo{}
	err := json.Unmarshal([]byte(layerHistory.V1Compatibility), &layerInfo)
	if err != nil {
		log.Fatalf("Could not unmarshal V1 compatibility information")
	}

	return layerInfo
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
