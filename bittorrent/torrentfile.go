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
	"os"

	"github.com/jackpal/bencode-go"
)

// updateTorrentFile updates the torrent file found at the given path, removing the web seeds
// and/or trackers.
func updateTorrentFile(torrentPath string, clearWebSeeds bool, clearTrackers bool) error {
	torrentFile, err := os.Open(torrentPath)
	if err != nil {
		torrentFile.Close()
		return err
	}

	result, berr := bencode.Decode(torrentFile)
	if berr != nil {
		torrentFile.Close()
		return err
	}

	torrentFile.Close()
	benmap := result.(map[string]interface{})
	if clearWebSeeds {
		delete(benmap, "url-list")
	}

	if clearTrackers {
		delete(benmap, "announce")
	}

	writeTorrentFile, err := os.OpenFile(torrentPath, os.O_WRONLY|os.O_TRUNC, 0777)
	if err != nil {
		writeTorrentFile.Close()
		return err
	}

	werr := bencode.Marshal(writeTorrentFile, benmap)
	if werr != nil {
		writeTorrentFile.Close()
		return err
	}
	writeTorrentFile.Close()

	return nil
}
