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

// Package dockerclient provides helper methods for creating a synthesized
// docker load TAR stream and loading it into the local Docker daemon.
package dockerclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	logrus "github.com/Sirupsen/logrus"

	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/context"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/version"
	"github.com/docker/docker/reference"
	"github.com/fsouza/go-dockerclient"

	"github.com/docker/distribution/registry/handlers"
	"github.com/docker/distribution/registry/listener"
	"github.com/docker/distribution/registry/storage/driver/factory"
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
func DockerLoad(image reference.Named, manifest *schema1.SignedManifest, layerPaths map[string]string, localIp string) error {
	go func() {
		err := runRegistry(image, manifest, layerPaths)
		if err != nil {
			log.Fatalf("Error running local registry: %v", err)
		}
	}()

	// Connect to Docker.
	log.Println("Connecting to docker")
	client, err := newDockerClient()
	if err != nil {
		return fmt.Errorf("Could not connect to Docker: %v", err)
	}

	// Wait a bit for the registry to start.
	time.Sleep(2 * time.Second)

	// Conduct a pull of the image.
	log.Println("Pulling image")

	var tagName = "latest"
	if tagged, ok := image.(reference.NamedTagged); ok {
		tagName = tagged.Tag()
	}

	w := newPullProgressDisplay(tagName, len(layerPaths))
	defer w.Done()

	localRegistry := fmt.Sprintf("%s:5000", localIp)
	localRepository := fmt.Sprintf("%s/%s", localRegistry, image.RemoteName())

	opts := docker.PullImageOptions{
		Repository:    localRepository,
		Registry:      localRegistry,
		Tag:           tagName,
		OutputStream:  w,
		RawJSONStream: true,
	}

	auth := docker.AuthConfiguration{}
	perr := client.PullImage(opts, auth)
	if perr != nil {
		return fmt.Errorf("Error pulling image into Docker: %v", perr)
	}

	// Tag the image to the name expected.
	tagOpts := docker.TagImageOptions{
		Repo:  image.FullName(),
		Tag:   tagName,
		Force: true,
	}

	localName := fmt.Sprintf("%s:%s", localRepository, tagName)
	terr := client.TagImage(localName, tagOpts)
	if terr != nil {
		return fmt.Errorf("Error re-tagging image in Docker: %v", terr)
	}

	// Untag the image with its temporary name.
	rerr := client.RemoveImage(localName)
	if rerr != nil {
		return fmt.Errorf("Error removing older tag in Docker: %v", rerr)

	}

	return nil
}

func runRegistry(image reference.Named, manifest *schema1.SignedManifest, layerPaths map[string]string) error {
	factory.Register("localserve", &localServeDriverFactory{
		image:      image,
		manifest:   manifest,
		layerPaths: layerPaths,
	})

	buf := bytes.NewBufferString(`
version: 0.1
log:
  level: error
  formatter: text
http:
  addr: localhost:5000
storage:
  localserve:
compatibility:
  schema1:
    disablesignaturestore: true
`)

	logrus.SetLevel(logrus.PanicLevel)

	ctx := context.WithVersion(context.Background(), version.Version)
	config, err := configuration.Parse(buf)
	if err != nil {
		panic(err)
	}

	handler := handlers.NewApp(ctx, config)
	server := &http.Server{
		Handler: handler,
	}

	ln, err := listener.NewListener(config.HTTP.Net, config.HTTP.Addr)
	if err != nil {
		return err
	}

	return server.Serve(ln)
}
