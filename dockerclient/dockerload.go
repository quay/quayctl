// dockerclient package provides helper methods for creating a synthesized docker load TAR stream
// and loading it into the local Docker daemon.
package dockerclient

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/reference"
	"github.com/dustin/go-humanize"
	"github.com/fsouza/go-dockerclient"
)

// V1LayerInfo holds information derived from a V1 history JSON blob.
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

// DockerLoadTar performs a `docker load` of a TAR containing the V1 docker load format.
func DockerLoadTar(reader io.Reader) error {
	client, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("Could not connect to Docker: %v", err)
	}

	opts := docker.LoadImageOptions{reader}
	lerr := client.LoadImage(opts)
	if lerr != nil {
		return fmt.Errorf("Could not perform docker-load: %v", lerr)
	}

	return nil
}

// DockerLoad performs a `docker load` of the given image with its manifest and layerPaths.
func DockerLoad(image reference.Named, manifest *schema1.SignedManifest, layerPaths map[string]string) error {
	log.Println("Performing docker load")

	// Connect to Docker.
	client, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("Could not connect to Docker: %v", err)
	}

	// Estimate the total size of the layers.
	var estimatedSize = uint64(0)
	for _, layerPath := range layerPaths {
		layerFile, ferr := os.Open(layerPath)
		if ferr != nil {
			return ferr
		}

		stat, serr := layerFile.Stat()
		if serr != nil {
			layerFile.Close()
			return serr
		}

		estimatedSize = estimatedSize + uint64(stat.Size())
		layerFile.Close()
	}

	// Create a new channeled reader-writer to stream the layer data.
	rw := newChanneledRW()
	opts := docker.LoadImageOptions{rw}
	readingComplete := make(chan struct{})

	// Start writing the combined TAR to the writer.
	go func() {
		terr := buildDockerLoadTar(image, manifest, layerPaths, rw)
		if terr != nil {
			panic(terr)
		}

		rw.DoneWriting()
	}()

	// Display a progressbar to track how much data has been read.
	progressBar := pb.New(100).Prefix("Streaming layer data to Docker: ")
	progressBar.SetMaxWidth(80)
	progressBar.ShowCounters = false
	progressBar.AlwaysUpdate = true

	pool, _ := pb.StartPool(progressBar)

	go func() {
		for {
			select {
			case <-readingComplete:
				return

			case <-time.After(250 * time.Millisecond):
				progressBar.Set(int((float64(rw.ReadCount()) / float64(estimatedSize)) * 100))
				progressBar.Postfix(fmt.Sprintf("%v / %v", humanize.Bytes(rw.ReadCount()), humanize.Bytes(estimatedSize)))
			}
		}
	}()

	// Call load with the reader.
	lerr := client.LoadImage(opts)
	readingComplete <- struct{}{}
	pool.Stop()

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
	topLayerId := GetLayerInfo(manifest.History[0]).ID
	tagMap := map[string]string{}
	tagMap[manifest.Tag] = topLayerId

	repositoriesMap := map[string]interface{}{}
	repositoriesMap[image.Hostname()+"/"+image.RemoteName()] = tagMap

	jsonString, _ := json.Marshal(repositoriesMap)
	writeTarFile(tw, "repositories", []byte(jsonString))

	// For each layer in the manifest, write a folder containing its JSON information, as well
	// as its layer TAR.
	for index, layer := range manifest.FSLayers {
		layerInfo := GetLayerInfo(manifest.History[index])

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
