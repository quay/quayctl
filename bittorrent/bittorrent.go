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

	"github.com/dmartinpro/libtorrent-go"
)

// alertPollInterval defines the time in milliseconds between each libtorrent alert poll.
const alertPollInterval = 250

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
	handle     libtorrent.Torrent_handle
	isFinished chan struct{}
}

// ClientFingerprint represents information about a client and its version.
// It is used to encode this information into the client's peer id.
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
	// libtorrent does.
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
	sessionFlags := int(libtorrent.SessionAdd_default_plugins)
	session := libtorrent.NewSession(fingerprint, sessionFlags)

	// Load all extensions.
	session.Add_extensions()

	// Enable alerts.
	var alertMask libtorrent.LibtorrentAlertCategory_t

	// status_notification is required, it is used to determine when a torrent is finished.
	alertMask |= libtorrent.AlertStatus_notification

	// error_notification is good to have at this point because the only error management that we do
	// is at the moment when we start to listen and add a torrent. There is not error management
	// except that. At least, we can output the errors to the user.
	alertMask |= libtorrent.AlertError_notification

	// For debug purposes, also enable these alerts:
	if config.Debug {
		alertMask |= libtorrent.AlertPeer_notification
		alertMask |= libtorrent.AlertStorage_notification
		alertMask |= libtorrent.AlertTracker_notification
		alertMask |= libtorrent.AlertStats_notification
		alertMask |= libtorrent.AlertPort_mapping_notification
		alertMask |= libtorrent.AlertError_notification
	}
	session.Set_alert_mask(uint(alertMask))

	// Configure client.
	// Reference: http://www.rasterbar.com/products/libtorrent/reference-Settings.html
	settings := session.Settings()
	settings.SetAnnounce_to_all_tiers(true)
	settings.SetAnnounce_to_all_trackers(true)
	settings.SetPeer_connect_timeout(2)
	settings.SetRate_limit_ip_overhead(true)
	settings.SetRequest_timeout(5)
	settings.SetTorrent_connect_boost(config.ConnectionsPerSecond * 10)
	settings.SetConnection_speed(config.ConnectionsPerSecond)
	if config.MaxDownloadRate > 0 {
		settings.SetDownload_rate_limit(config.MaxDownloadRate)
	}
	if config.MaxUploadRate > 0 {
		settings.SetUpload_rate_limit(config.MaxUploadRate)
	}
	session.Set_settings(settings)

	// Configure encryption policies.
	encryptionSettings := libtorrent.NewPe_settings()
	encryptionSettings.SetOut_enc_policy(byte(config.Encryption))
	encryptionSettings.SetIn_enc_policy(byte(config.Encryption))
	encryptionSettings.SetAllowed_enc_level(byte(libtorrent.Pe_settingsBoth))
	encryptionSettings.SetPrefer_rc4(true)
	session.Set_pe_settings(encryptionSettings)

	return &Client{
		session:  session,
		torrents: make(map[string]*torrent),
		config:   config,
	}
}

// Start launches the configured Client and makes it ready to accept torrents.
func (bt *Client) Start() error {
	// Start services.
	bt.session.Start_upnp()
	bt.session.Start_natpmp()
	bt.session.Start_lsd()

	// Listen.
	errCode := libtorrent.NewError_code()
	defer libtorrent.DeleteError_code(errCode)

	ports := libtorrent.NewStd_pair_int_int(bt.config.LowerListenPort, bt.config.UpperListenPort)
	defer libtorrent.DeleteStd_pair_int_int(ports)

	bt.session.Listen_on(ports, errCode)
	if errCode.Value() != 0 {
		return fmt.Errorf("Unable to start the Bittorrent client: error code %v, %v", errCode.Value(), errCode.Message())
	}

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
	bt.session.Stop_lsd()
	bt.session.Stop_upnp()
	bt.session.Stop_natpmp()

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

	bt.torrentsLock.Lock()

	// Verify that the torrent is unique first, otherwise we'll have trouble detecting the finished
	// state.
	if _, found := bt.torrents[sourcePath]; found {
		bt.torrentsLock.Unlock()
		return "", nil, errors.New("This torrent is already being downloaded.")
	}

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

		resp, err := http.Get(torrentPath)
		if err != nil {
			return "", nil, fmt.Errorf("Unable to start torrent: could not download .torrent file.")
		}
		defer resp.Body.Close()

		io.Copy(f, resp.Body)
		f.Close()

		torrentPath = f.Name()
	}

	// Create torrent parameters.
	torrentParams := libtorrent.NewAdd_torrent_params()
	if strings.HasPrefix(torrentPath, "magnet:") {
		torrentParams.SetUrl(torrentPath)
	} else {
		torrentInfo := libtorrent.NewTorrent_info(torrentPath)
		torrentParams.Set_torrent_info(torrentInfo)
	}
	torrentParams.SetSave_path(downloadPath)

	// Set flags to 0 to disable auto-management !
	torrentParams.SetFlags(0)

	// Add torrent to the Bittorrent client.
	errCode := libtorrent.NewError_code()
	defer libtorrent.DeleteError_code(errCode)

	handle := bt.session.Add_torrent(torrentParams)
	if errCode.Value() != 0 {
		return "", nil, fmt.Errorf("Unable to start torrent: error code %v, %v", errCode.Value(), errCode.Message())
	}
	bt.torrents[sourcePath] = &torrent{handle: handle, isFinished: make(chan struct{})}

	bt.torrentsLock.Unlock()

	// Wait for the download to finish.
	<-bt.torrents[sourcePath].isFinished
	path := path.Clean(downloadPath + "/" + handle.Torrent_file().Name())

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

func (bt *Client) deleteTorrent(sourcePath string, keepSeedingChan chan struct{}) {
	if torrent, found := bt.torrents[sourcePath]; found {
		delete(bt.torrents, sourcePath)
		bt.session.Remove_torrent(torrent.handle, 0)
	}
	if keepSeedingChan != nil {
		close(keepSeedingChan)
	}
}

// alertsConsumer handles notifications that libtorrent sends.
// At the moment, it is only used to mark a torrent as finished.
func (bt *Client) alertsConsumer() {
	for bt.Running {
		if bt.session.Wait_for_alert(libtorrent.Milliseconds(alertPollInterval)).Swigcptr() != 0 {
			alert := bt.session.Pop_alert()
			switch alert.Xtype() {
			case libtorrent.Torrent_finished_alertAlert_type:
				handle := libtorrent.SwigcptrTorrent_finished_alert(alert.Swigcptr()).GetHandle()
				if torrent := bt.findTorrent(handle); torrent != nil {
					close(torrent.isFinished)
				} else {
					log.Printf("bittorrent: Unknown torrent %v finished", handle.Info_hash())
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
func (bt *Client) findTorrent(torrent libtorrent.Torrent_handle) *torrent {
	bt.torrentsLock.Lock()
	defer bt.torrentsLock.Unlock()

	for _, t := range bt.torrents {
		if torrent.Equal(t.handle) {
			return t
		}
	}
	return nil
}
