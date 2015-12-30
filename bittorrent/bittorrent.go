package bittorrent

import (
	"errors"
	"math"
	"time"

	"github.com/jackpal/Taipei-Torrent/torrent"
)

// Client is a BitTorrent client wrapper around Taipei-Torrent, that exposes
// the torrent structures in order to give us a (more) fine-grained control over them.
// More specifically, it allows us to be notified when a torrent download finishes and to keep
// seeding for a specified duration after that.
//
// However, please note that it drops LPD and DHT support.
type Client struct {
	// Internal config for Taipei-Torrent
	flags *torrent.TorrentFlags
	// Listening port for peers
	listenPort int

	// Torrents to start
	startingTorrents chan *torrent.TorrentSession
	// Current torrents
	activeTorrents map[string]*torrent.TorrentSession
	// Torrents to stop
	stoppingTorrents chan *torrent.TorrentSession

	// Shutdown
	running     bool
	quitChan    chan struct{}
	stoppedChan chan struct{}
}

// NewClient initializes a new Bittorrent client.
func NewClient(listenPort int, downloadDir string) (*Client, error) {
	flags := &torrent.TorrentFlags{
		Port:               listenPort,
		FileDir:            downloadDir,
		SeedRatio:          math.Inf(0),
		InitialCheck:       true,
		FileSystemProvider: torrent.OsFsProvider{},
	}

	bt := &Client{
		flags: flags,
	}

	return bt, nil
}

// Run starts the Bittorrent client, which will then be able to download .
func (bt *Client) Run() error {
	// Initialize data structures.
	bt.startingTorrents = make(chan *torrent.TorrentSession, 1)
	bt.activeTorrents = make(map[string]*torrent.TorrentSession)
	bt.stoppingTorrents = make(chan *torrent.TorrentSession, 1)
	bt.running = true
	bt.quitChan = make(chan struct{})
	bt.stoppedChan = make(chan struct{})

	// Start listening for peer.
	conChan, listenPort, err := torrent.ListenForPeerConnections(bt.flags)
	if err != nil {
		return err
	}
	bt.listenPort = listenPort

mainLoop:
	for {
		select {
		case ts := <-bt.startingTorrents:
			if bt.running {
				bt.activeTorrents[ts.M.InfoHash] = ts
				go func(t *torrent.TorrentSession) {
					t.DoTorrent()
					bt.stoppingTorrents <- t
				}(ts)
			}
		case ts := <-bt.stoppingTorrents:
			delete(bt.activeTorrents, ts.M.InfoHash)

			if bt.running == false && len(bt.activeTorrents) == 0 {
				break mainLoop
			}
		case c := <-conChan:
			if ts, ok := bt.activeTorrents[c.Infohash]; ok {
				go ts.AcceptNewPeer(c)
			}
		case <-bt.quitChan:
			bt.running = false
			if len(bt.activeTorrents) == 0 {
				break mainLoop
			}
			for _, ts := range bt.activeTorrents {
				go ts.Quit()
			}
		}
	}

	close(bt.stoppedChan)

	return nil
}

// Download submits a new torrent to be downloaded.
//
// The provided torrent must either be a magnet link, a local file path or an
// HTTP URL to a .torrent file.
//
// The function blocks until the torrent is fully downloaded and then returns the path where the
// downloaded content sits.
//
// Once the torrent has been downloaded, it will keep being seeded for the specified amount of time,
// the returned channel will be closed at the end of the seeding period.
//
func (bt *Client) Download(t string, seedDuration time.Duration) (string, chan struct{}, error) {
	if !bt.running {
		return "", nil, errors.New("Use Run() before Download()")
	}

	ts, err := torrent.NewTorrentSession(bt.flags, t, uint16(bt.listenPort))
	if err != nil {
		bt.stoppingTorrents <- &torrent.TorrentSession{}
		return "", nil, err
	}
	bt.startingTorrents <- ts

	// Wait for download to end.
	for {
		time.Sleep(300 * time.Millisecond)

		// Check if the download is finished.
		if ts.Session.HaveTorrent && ts.Session.Left == 0 {
			// Close torrent session, optionally after seeding for a while.
			keepSeedingChan := make(chan struct{})
			if seedDuration > 0 {
				go func() {
					time.Sleep(seedDuration)
					ts.Quit()
					close(keepSeedingChan)
				}()
			} else {
				ts.Quit()
				close(keepSeedingChan)
			}

			return bt.flags.FileDir + "/" + ts.M.Info.Name, keepSeedingChan, nil
		}
	}
}

// Shutdown stops a running Bittorrent client and all its active torrents.
func (bt *Client) Shutdown() {
	if !bt.running {
		return
	}
	bt.quitChan <- struct{}{}
	<-bt.stoppedChan
}
