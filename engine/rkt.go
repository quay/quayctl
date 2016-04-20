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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"

	"github.com/appc/spec/discovery"
	"github.com/spf13/cobra"
)

// RktEngine defines an engine interface for interacting with rkt.
type RktEngine struct{}

func (re RktEngine) Name() string {
	return "rkt"
}

func (re RktEngine) Title() string {
	return "Commands for pulling images into rkt from Quay"
}

func (re RktEngine) TorrentHandler() engineTorrentHandler {
	return &rktTorrentHandler{}
}

type rktContext struct {
	signatureUrl *url.URL
}

// rktConfig is a structure representing the data that is returned by the `rkt config` command.
type rktConfig struct {
	Stage0 []stage0config `json:"stage0"`
}

type rktKind string

const (
	rktKindAuth       rktKind = "auth"
	rktKindDockerAuth         = "dockerAuth"
	rktKindPaths              = "paths"
	rktKindStage1             = "stage1"
)

type rktAuthType string

const (
	rktAuthBasic rktAuthType = "basic"
	rktAuthToken             = "token"
)

// stage0config is the config for rkt stage0.
type stage0config struct {
	RktKind     rktKind        `json:"rktKind"`
	Domains     []string       `json:"domains"`
	AuthType    rktAuthType    `json:"type"`
	Credentials rktCredentials `json:"credentials"`
}

// rktCredentials represents credentials stored for use by rkt.
type rktCredentials struct {
	Username string `json:"user"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

// rktTorrentHandler defines an interface for pulling a rkt image via torrent.
type rktTorrentHandler struct{}

func (rth rktTorrentHandler) DecorateCommand(command *cobra.Command) {}

func (rth rktTorrentHandler) RetrieveTorrents(image string, insecureFlag bool, option layersOption) ([]torrentInfo, interface{}, error) {
	// Parse the image string.
	app, err := discovery.NewAppFromString(image)
	if err != nil {
		return []torrentInfo{}, nil, err
	}

	if _, ok := app.Labels["arch"]; !ok {
		app.Labels["arch"] = runtime.GOARCH
	}
	if _, ok := app.Labels["os"]; !ok {
		app.Labels["os"] = runtime.GOOS
	}

	// Perform discovery for the image.
	var insecureOption = discovery.InsecureNone
	if insecureFlag {
		insecureOption = discovery.InsecureHTTP
	}

	log.Printf("Discovering image %v", image)
	endpoints, _, err := discovery.DiscoverACIEndpoints(*app, nil, insecureOption)
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could not discover %v: %v", app, err)
	}

	// Build the URL for the ACI image.
	aciUrl, err := url.Parse(endpoints[0].ACI)
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could not download %v: %v", app, err)
	}

	signatureUrl, err := url.Parse(endpoints[0].ASC)
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could not download %v: %v", app, err)
	}

	if insecureFlag {
		aciUrl.Scheme = "http"
		signatureUrl.Scheme = "http"
	}

	// Find any auth credentials for the requests.
	cmd := exec.Command("rkt", "config")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could call rkt config: %v", err)
	}

	// Unmarshal the configuration data.
	topLevel := rktConfig{}
	err = json.Unmarshal(data, &topLevel)
	if err != nil {
		return []torrentInfo{}, nil, fmt.Errorf("Could unmarshal rkt config data: %v", err)
	}

	// Search for auth for the domain.
	for _, config := range topLevel.Stage0 {
		if config.RktKind == rktKindAuth && config.AuthType == rktAuthBasic {
			for _, domain := range config.Domains {
				if domain == aciUrl.Host {
					log.Printf("Found credentials for image %v", image)
					aciUrl.User = url.UserPassword(config.Credentials.Username, config.Credentials.Password)
					signatureUrl.User = url.UserPassword(config.Credentials.Username, config.Credentials.Password)
				}
			}
		}
	}

	log.Printf("Downloading torrent for image %v", image)
	torrent := torrentInfo{
		id:          "aci",
		torrentPath: aciUrl.String(),
		title:       image,
	}

	return []torrentInfo{torrent}, rktContext{signatureUrl}, nil
}

func (rth rktTorrentHandler) LoadImage(image string, downloadInfo downloadTorrentInfo, ctx interface{}) error {
	// Wait for the torrent to complete.
	<-downloadInfo.CompleteChannel

	// Download the signature.
	log.Printf("Downloading signature for image %v", image)
	aciPath, _ := downloadInfo.TorrentPaths.Get("aci")
	signaturePath := fmt.Sprintf("%s.aci.asc", aciPath)
	err := downloadFile(ctx.(rktContext).signatureUrl, signaturePath)
	if err != nil {
		return fmt.Errorf("Could not download signature for image %v: %v", image, err)
	}

	// Load the image into rkt via a fetch of the local file.
	log.Printf("Loading image %v", image)
	aciLocalPath := url.URL{
		Scheme: "file",
		Path:   aciPath.(string),
	}

	cmd := exec.Command("rkt", "fetch", aciLocalPath.String(), "--trust-keys-from-https=true")
	cmdReader, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("Could not load image %v into rkt: %v", image, err)
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("Could not load image %v into rkt: %v", image, err)
	}

	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("Could not load image %v into rkt: %v", image, err)
	}

	return nil
}

func downloadFile(url *url.URL, filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	resp, err := http.Get(url.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
