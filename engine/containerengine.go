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
	"github.com/spf13/cobra"
)

// layersOption specifies an option to the RetrieveTorrents call on whether to download
// all layers (even if already present within the engine) or just those missing.
type layersOption int

const (
	// allLayers specifies that torrents should be returned for all layers, regardless of
	// whether they are present within the container engine's layer store.
	AllLayers layersOption = iota

	// missingLayers specifies that torrents should be returned for only those layers missing
	// within the container engine's layer store.
	MissingLayers
)

// ContainerEngine represents a container engine (e.g. Docker or rkt) with which quayctl
// can interact.
type ContainerEngine interface {
	// Name is a single identifier for the engine, used as the first parameter
	// on the quayctl command line.
	Name() string

	// Title is a human-readable title for the engine.
	Title() string

	// TorrentHandler returns a handler for interacting with the `torrent pull` command.
	TorrentHandler() engineTorrentHandler
}

// engineTorrentHandler represents the handling of the `torrent pull` command for a specific
// container engine.
type engineTorrentHandler interface {
	// DecorateCommand is called to decorate the `torrent pull` command with any custom flags
	// needed by this container engine.
	DecorateCommand(command *cobra.Command)

	// RetrieveTorrents retrieves all the torrents to be downloaded for the container image.
	RetrieveTorrents(image string, insecureFlag bool, option layersOption) ([]torrentInfo, interface{}, error)

	// LoadImage performs the loading of the downloaded container image into the container
	// engine.
	LoadImage(image string, downloadInfo downloadTorrentInfo, ctx interface{}) error
}
