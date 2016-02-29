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

package bittorrent

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/coreos/libtorrent-go"
)

// Client wraps libtorrent and allows us to download torrents easily.
type Client struct {
	// Running reports the status of the underlying libtorrent session.
	// It is set to true after a successfull Start() and set to false when Stop() is called.
	// Using Download() only makes sense when Running equals true.
	Running bool

	// The main libtorrent object.
	session libtorrent.Session

	// Contains the active torrents' handles.
	torrents     map[string]*torrent
	torrentsLock sync.Mutex

	// Refers to the configuration that has been used in NewClient to configure libtorrent.
	config ClientConfig
}

// torrent stores the libtorrent handle referring an active torrent and a channel that is closed
// once the torrent's download is finished.
type torrent struct {
	handle     libtorrent.TorrentHandle
	isFinished chan struct{}
}

// Status contains several pieces of information about the status of a torrent.
type Status struct {
	// Name is the torrent's name.
	Name string

	// Status represents the current torrent's state.
	Status TorrentState

	// Progress is download completion percentage.
	Progress float32

	// DownloadRate is the total download rates for all peers for this torrent, expressed in kB/s.
	DownloadRate float32

	// UploadRate is the total upload rates for all peers for this torrent, expressed in kB/s.
	UploadRate float32

	// NumConnectCandidates is the number of peers in this torrent's peer list that is a candidate
	// to be connected to. i.e. It has fewer connect attempts than the max fail count, it is not a
	// seed if we are a seed, it is not banned etc.
	// If this is 0, it means we don't know of any more peers that we can try.
	NumConnectCandidates int

	// NumPeeers is the total number of peer connections this session has. This includes incoming
	// connections that still hasn't sent their handshake or outgoing connections that still hasn't
	// completed the TCP connection.
	NumPeers int

	// NumSeeds is the number of peers that are seeding that this client is currently connected to.
	NumSeeds int
}

// TorrentState represents a torrent's current task.
type TorrentState string

const (
	// alertPollInterval defines the time in milliseconds between each libtorrent alert poll.
	alertPollInterval = 250

	// QueuedForChecking means that the torrent is in the queue for being checked. But there currently
	// is another torrent that are being checked. This torrent will wait for its turn.
	QueuedForChecking TorrentState = "Queued for checking"

	// CheckingFiles means that the torrent has not started its download yet, and is currently
	// checking existing files.
	CheckingFiles = "Checking files"

	// DownloadingMetadata means that the torrent is trying to download metadata from peers.
	DownloadingMetadata = "Downloading metadata"

	// Downloading means that the torrent is being downloaded. This is the state most torrents will be
	// in most of the time.
	Downloading = "Downloading"

	// Finished means that the torrent has finished downloading but still doesn't have the entire torrent.
	// i.e. some pieces are filtered and won't get downloaded.
	// As this library doesn't allow filtering, the state is transient.
	Finished = "Finished"

	// Seeding means that torrent has finished downloading and is a pure seeder.
	Seeding = "Seeding"

	// Allocating means that if the torrent was started in full allocation mode, this indicates
	// that the (disk) storage for the torrent is allocated.
	// As this library only uses the default allocation mode (sparse allocation), this state should
	// never appear.
	Allocating = "Allocating"

	// CheckingResumeData means that the torrent is currently checking the fastresume data and
	// comparing it to the files on disk. This is typically completed in a fraction of a second,
	// but if you add a large number of torrents at once, they will queue up.
	CheckingResumeData = "Checking resume data"

	// Unknown probably means that we couldn't get the current state information or that libtorrent
	// introduced a new state that we don't recognize yet.
	Unknown = "Unknown"
)

// ClientFingerprint represents information about a client and its version.
// It is encoded into the client's peer id.
type ClientFingerprint struct {
	// ID represents uniquely a client. It must contains exactly two characters.
	// Unofficial list: https://wiki.theory.org/BitTorrentSpecification#peer_id
	ID string

	// Major represents the major version of the client. It must be within the range [0, 9].
	Major int

	// Minor represents the minor version of the client. It must be within the range [0, 9].
	Minor int

	// Revision represents the revision of the client. It must be within the range [0, 9].
	Revision int

	// Tag represents the version tag of the client. It must be within the range [0, 9].
	Tag int
}

// ClientConfig represents the configuration that can be passed to NewClient to
// configure libtorrent.
type ClientConfig struct {
	// Fingerprint represents information about a client and its version.
	// It is used to encode this information into the client's peer id.
	Fingerprint ClientFingerprint

	// LowerListenPort defines the lowest port on which libtorrent will try to listen.
	LowerListenPort int

	// UpperListenPort defines the highest port on which libtorrent will try to listen.
	UpperListenPort int

	// ConnectionsPerSecond specifies the maximum number of outgoing connections per second
	// libtorrent allows.
	ConnectionsPerSecond int

	// MaxDownloadRate defines the maximun bandwidth (in bytes/s) that libtorrent will use to download
	// torrents. A zero value mean unlimited.
	// Note that it does not apply for peers on the local network, which are not rate limited.
	MaxDownloadRate int

	// MaxUploadRate defines the maximun bandwidth (in bytes/s) that libtorrent will use to upload
	// torrents. A zero value mean unlimited.
	// Note that it does not apply for peers on the local network, which are not rate limited.
	MaxUploadRate int

	// Encryption controls the peer protocol encryption policies.
	Encryption EncryptionMode

	// Debug, when set to true, makes libtorrent output every available alert.
	Debug bool
}

// EncryptionMode is the type that control the settings related to peer protocol encryption
// in libtorrent.
type EncryptionMode int

const (
	// FORCED allows only encrypted connections. Incoming connections that are not encrypted are
	// closed and if the encrypted outgoing connection fails, a non-encrypted retry will not be made.
	FORCED EncryptionMode = 0

	// ENABLED allows both encrypted and non-encryption connections.
	// An incoming non-encrypted connection will be accepted, and if an outgoing encrypted
	// connection fails, a non- encrypted connection will be tried.
	ENABLED = 1

	// DISABLED only allows only non-encrypted connections.
	DISABLED = 2
)

// NewClient initializes a new Bittorrent client using the specified configuration.
func NewClient(config ClientConfig) *Client {
	// Create session.
	fingerprint := libtorrent.NewFingerprint(config.Fingerprint.ID, config.Fingerprint.Major,
		config.Fingerprint.Minor, config.Fingerprint.Revision, config.Fingerprint.Tag)
	session := libtorrent.NewSession(fingerprint, int(libtorrent.SessionAddDefaultPlugins))

	// Configure client.
	// Reference: http://www.rasterbar.com/products/libtorrent/reference-Settings.html
	settings := session.Settings()
	settings.SetAnnounceToAllTiers(true)
	settings.SetAnnounceToAllTrackers(true)
	settings.SetPeerConnectTimeout(2)
	settings.SetRateLimitIpOverhead(true)
	settings.SetRequestTimeout(5)
	settings.SetTorrentConnectBoost(config.ConnectionsPerSecond * 10)
	settings.SetConnectionSpeed(config.ConnectionsPerSecond)
	settings.SetDownloadRateLimit(config.MaxDownloadRate)
	settings.SetUploadRateLimit(config.MaxUploadRate)
	session.SetSettings(settings)

	// Configure encryption policies.
	encryptionSettings := libtorrent.NewPeSettings()
	defer libtorrent.DeletePeSettings(encryptionSettings)
	encryptionSettings.SetOutEncPolicy(byte(config.Encryption))
	encryptionSettings.SetInEncPolicy(byte(config.Encryption))
	encryptionSettings.SetAllowedEncLevel(byte(libtorrent.PeSettingsBoth))
	encryptionSettings.SetPreferRc4(true)
	session.SetPeSettings(encryptionSettings)

	// Enable alerts.
	// - status_notification is used to determine when a torrent is finished.
	// - error_notification is good to have at this point because the only error management that we do
	//   is at the moment when we start to listen and add a torrent. There is not error management
	//   except that. At least, we can output the errors to the user.
	alertMask := libtorrent.AlertStatusNotification | libtorrent.AlertErrorNotification
	if config.Debug {
		alertMask = libtorrent.AlertAllCategories
	}

	session.SetAlertMask(uint(alertMask))

	// Load all extensions.
	session.AddExtensions()

	return &Client{
		session:  session,
		torrents: make(map[string]*torrent),
		config:   config,
	}
}

// Start launches the configured Client and makes it ready to accept torrents.
func (bt *Client) Start() error {
	// Listen.
	errCode := libtorrent.NewErrorCode()
	defer libtorrent.DeleteErrorCode(errCode)

	ports := libtorrent.NewStdPairIntInt(bt.config.LowerListenPort, bt.config.UpperListenPort)
	defer libtorrent.DeleteStdPairIntInt(ports)

	bt.session.ListenOn(ports, errCode)
	if errCode.Value() != 0 {
		return fmt.Errorf("Unable to start the Bittorrent client: error code %v, %v", errCode.Value(), errCode.Message())
	}

	// Start services.
	bt.session.StartUpnp()
	bt.session.StartNatpmp()
	bt.session.StartLsd()

	bt.Running = true

	// Start alert monitoring.
	go bt.alertsConsumer()

	return nil
}

// Stop interrupts every active torrents and destroy the libtorrent session.
func (bt *Client) Stop() {
	bt.Running = false

	// Stop torrents.
	bt.torrentsLock.Lock()
	for sourcePath := range bt.torrents {
		bt.deleteTorrent(sourcePath, nil)
	}
	bt.torrentsLock.Unlock()

	// Stop services.
	bt.session.StopLsd()
	bt.session.StopUpnp()
	bt.session.StopNatpmp()

	// Delete session.
	libtorrent.DeleteSession(bt.session)
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
// There are three cases:
// - seedDuration == nil, no seeding: the torrent is removed right away and keepSeedingChan
// is closed.
// - seedDuration > 0, seed for the specified duration: the torrent will be removed and
// keepSeedingChan closed after that duration.
// - seedDuration == 0, seed forever: the torrent will not be removed and keepSeedingChan will not
// be closed until Stop() is called.
func (bt *Client) Download(sourcePath, downloadPath string, seedDuration *time.Duration) (string, chan struct{}, error) {
	if !bt.Running {
		return "", nil, errors.New("Use Start() before Download()")
	}

	// Verify that the torrent is unique first, otherwise we'll have trouble detecting the finished
	// state.
	bt.torrentsLock.Lock()
	if _, found := bt.torrents[sourcePath]; found {
		bt.torrentsLock.Unlock()
		return "", nil, errors.New("This torrent is already being downloaded.")
	}
	bt.torrentsLock.Unlock()

	// Download .torrent file.
	//
	// An issue in libtorrent prevents it from using web seeds when torrents are added by URLs.
	// As a workaround, we download the .torrent files to temp files and pass them to libtorrent.
	torrentPath := sourcePath
	if strings.HasPrefix(torrentPath, "http://") || strings.HasPrefix(torrentPath, "https://") {
		f, err := ioutil.TempFile("", "quayctl-torrent")
		if err != nil {
			return "", nil, fmt.Errorf("Unable to start torrent: could not create temp file for .torrent.")
		}
		defer os.Remove(f.Name())

		client := http.DefaultClient
		request, err := http.NewRequest("GET", torrentPath, nil)
		if err != nil {
			return "", nil, err
		}

		request.Header.Add("Accept", "application/x-bittorrent")

		resp, err := client.Do(request)
		if err != nil {
			return "", nil, fmt.Errorf("Unable to start torrent: could not download .torrent file.")
		}

		if resp.StatusCode/100 >= 4 {
			return "", nil, fmt.Errorf("Unable to start torrent: got %v for .torrent file", resp.StatusCode)
		}

		defer resp.Body.Close()

		io.Copy(f, resp.Body)
		f.Close()

		torrentPath = f.Name()
	}

	// Create torrent parameters.
	torrentParams := libtorrent.NewAddTorrentParams()
	if strings.HasPrefix(torrentPath, "magnet:") {
		torrentParams.SetUrl(torrentPath)
	} else {
		torrentInfo := libtorrent.NewTorrentInfo(torrentPath)
		torrentParams.SetTorrentInfo(torrentInfo)
	}
	torrentParams.SetSavePath(downloadPath)

	// Set flags to 0 to disable auto-management !
	torrentParams.SetFlags(0)

	// Add torrent to the Bittorrent client.
	errCode := libtorrent.NewErrorCode()
	defer libtorrent.DeleteErrorCode(errCode)

	bt.torrentsLock.Lock()
	if _, found := bt.torrents[sourcePath]; found {
		bt.torrentsLock.Unlock()
		return "", nil, errors.New("This torrent is already being downloaded.")
	}

	handle := bt.session.AddTorrent(torrentParams)
	if errCode.Value() != 0 {
		bt.torrentsLock.Unlock()
		return "", nil, fmt.Errorf("Unable to start torrent: error code %v, %v", errCode.Value(), errCode.Message())
	}

	torrent := &torrent{handle: handle, isFinished: make(chan struct{})}
	bt.torrents[sourcePath] = torrent
	bt.torrentsLock.Unlock()

	// Wait for the download to finish.
	<-torrent.isFinished
	path := path.Clean(downloadPath + "/" + handle.TorrentFile().Name())

	// Seed for the specified duration.
	keepSeedingChan := make(chan struct{})
	if seedDuration == nil {
		bt.torrentsLock.Lock()
		bt.deleteTorrent(sourcePath, keepSeedingChan)
		bt.torrentsLock.Unlock()
	} else if *seedDuration > 0 {
		go func() {
			time.Sleep(*seedDuration)
			bt.torrentsLock.Lock()
			bt.deleteTorrent(sourcePath, keepSeedingChan)
			bt.torrentsLock.Unlock()
		}()
	}

	return path, keepSeedingChan, nil
}

// GetStatus queries and returns several informations about the specified torrent.
// The torrent must be currently downloading or seed, an error will be thrown otherwise.
func (bt *Client) GetStatus(sourcePath string) (Status, error) {
	var s Status

	bt.torrentsLock.Lock()
	defer bt.torrentsLock.Unlock()

	torrent, found := bt.torrents[sourcePath]
	if !found {
		return s, errors.New("torrent not found")
	}
	status := torrent.handle.Status(uint(0))

	s.Name = torrent.handle.TorrentFile().Name()
	s.Status = parseTorrentState(status.GetState())
	s.Progress = status.GetProgress() * 100
	s.DownloadRate = float32(status.GetDownloadRate()) / 1024
	s.UploadRate = float32(status.GetUploadRate()) / 1024
	s.NumConnectCandidates = status.GetConnectCandidates()
	s.NumPeers = status.GetNumPeers()
	s.NumSeeds = status.GetNumSeeds()

	return s, nil
}

func parseTorrentState(state libtorrent.LibtorrentTorrent_statusState_t) TorrentState {
	switch state {
	case libtorrent.TorrentStatusQueuedForChecking:
		return QueuedForChecking
	case libtorrent.TorrentStatusCheckingFiles:
		return CheckingFiles
	case libtorrent.TorrentStatusDownloadingMetadata:
		return DownloadingMetadata
	case libtorrent.TorrentStatusDownloading:
		return Downloading
	case libtorrent.TorrentStatusFinished:
		return Finished
	case libtorrent.TorrentStatusSeeding:
		return Seeding
	case libtorrent.TorrentStatusAllocating:
		return Allocating
	case libtorrent.TorrentStatusCheckingResumeData:
		return CheckingResumeData
	default:
		return Unknown
	}
}

func (bt *Client) deleteTorrent(sourcePath string, keepSeedingChan chan struct{}) {
	if torrent, found := bt.torrents[sourcePath]; found {
		delete(bt.torrents, sourcePath)
		bt.session.RemoveTorrent(torrent.handle, 0)
	}
	if keepSeedingChan != nil {
		close(keepSeedingChan)
	}
}

// alertsConsumer handles notifications that libtorrent sends.
// At the moment, it is only used to mark a torrent as finished.
func (bt *Client) alertsConsumer() {
	for bt.Running {
		if bt.session.WaitForAlert(libtorrent.Milliseconds(alertPollInterval)).Swigcptr() != 0 {
			alert := bt.session.PopAlert()
			switch alert.Type() {
			case libtorrent.TorrentFinishedAlertAlertType:
				handle := libtorrent.SwigcptrTorrentFinishedAlert(alert.Swigcptr()).GetHandle()
				if torrent := bt.findTorrent(handle); torrent != nil {
					close(torrent.isFinished)
				} else {
					log.Printf("bittorrent: Unknown torrent %v finished", handle.InfoHash())
				}
			default:
				if bt.config.Debug {
					log.Printf("bittorrent: %s: %s", alert.What(), alert.Message())
				}
			}
		}
	}
}

// findTorrent finds the torrent in our torrent list that corresponds to the specified handle.
//
// This is necessary because when a torrent is added, we don't know anything about it except
// its .torrent file or its magnet link. So when libtorrent sends us a notification with an handle,
// we have no common index to retrieve our torrent structure easily. Thus, we use .Equal() which is
// an alias for the C++ == operator that will match the handle that we already have.
func (bt *Client) findTorrent(torrent libtorrent.TorrentHandle) *torrent {
	bt.torrentsLock.Lock()
	defer bt.torrentsLock.Unlock()

	for _, t := range bt.torrents {
		if torrent.Equal(t.handle) {
			return t
		}
	}
	return nil
}
