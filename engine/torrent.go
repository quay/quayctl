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
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/dustin/go-humanize"
	"github.com/streamrail/concurrent-map"

	"github.com/coreos/quayctl/bittorrent"
)

// torrentSeedOption defines the option for whether to seed after a layer has been downloaded
// via torrent.
type torrentSeedOption int

const (
	TorrentNoSeed torrentSeedOption = iota
	TorrentSeedAfterPull
)

// torrentInfo holds the blobSum and torrent path for a torrent.
type torrentInfo struct {
	id          string
	torrentPath string
	title       string
}

// downloadTorrentInfo contains data structures populated and signaled by the DownloadTorrents
// method.
type downloadTorrentInfo struct {
	DownloadedChannels map[string]chan struct{} // Map of torrent ID -> channel to await download
	CompleteChannel    chan struct{}            // Channel to await completion of all torrent ops
	Pool               *pb.Pool                 // ProgressBar pool
	HasProgressBars    bool                     // Whether progress bars are running.
	TorrentPaths       cmap.ConcurrentMap       // Map from torrent ID -> downloaded path
}

// DownloadTorrents starts the downloads of all the specified torrents, with optional seeding once
// completed. Returns immediately with a downloadTorrentInfo struct.
func DownloadTorrents(torrents []torrentInfo, torrentFolder string, seedOption torrentSeedOption,
	torrentSeedDuration time.Duration, clientConfig bittorrent.ClientConfig,
	downloadConfig bittorrent.DownloadConfig) downloadTorrentInfo {

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

	if clientConfig.Debug {
		pool.Stop()
		hasProgressBars = false
	}

	// Initialize Bittorrent client.
	bt, err := initBitTorrentClient(torrentFolder, clientConfig)
	if err != nil {
		panic(fmt.Errorf("Could not initialize torrent client: %v", err))
	}

	// Listen for Ctrl-C.
	go catchShutdownSignals(bt, pool, hasProgressBars)

	// For each torrent, download the data in parallel, call post-processing and (optionally)
	// seed.
	var localSeedDuration *time.Duration
	if seedOption == TorrentSeedAfterPull {
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
			path, keepSeeding, err := bt.Download(torrent.torrentPath, torrentFolder, localSeedDuration, downloadConfig)
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
func initBitTorrentClient(torrentFolder string, clientConfig bittorrent.ClientConfig) (*bittorrent.Client, error) {
	// Ensure destination folder exists.
	if err := os.MkdirAll(torrentFolder, 0755); err != nil {
		return nil, err
	}

	// Create client.
	bt := bittorrent.NewClient(clientConfig)

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
