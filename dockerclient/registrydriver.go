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

package dockerclient

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/docker/distribution/context"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
)

// localServeDriver implements the Docker Registry storage engine to serve the specified
// layer data.
type localServeDriver struct {
	contentPaths         map[string][]byte // Map of request path to direct data.
	externalContentPaths map[string]string // Map of request path to on-system files.
}

// addLink adds a link from a prefix to a blob.
func (d *localServeDriver) addLink(repository string, location string, digest string) {
	linkPath := fmt.Sprintf(
		"/docker/registry/v2/repositories/%s/%s",
		repository,
		location)

	d.contentPaths[linkPath] = []byte(digest)
}

func (d *localServeDriver) addDigestLink(repository, prefix string, digest string) {
	hexSha := digest[len("sha256:"):]
	d.addLink(repository, fmt.Sprintf("%s/sha256/%s/link", prefix, hexSha), digest)
}

// addLinkedFile adds a linked external file to the driver.
func (d *localServeDriver) addLinkedFile(repository string, prefix string, digest string, filePath string) {
	// Define a link from the prefix-ed SHA to the SHA itself.
	d.addDigestLink(repository, prefix, digest)

	// Define the data path.
	hexSha := digest[len("sha256:"):]
	dataPath := fmt.Sprintf(
		"/docker/registry/v2/blobs/sha256/%s/%s/data",
		hexSha[0:2],
		hexSha)

	d.externalContentPaths[dataPath] = filePath
}

// addLinkedData adds a piece of linked data to the driver.
func (d *localServeDriver) addLinkedData(repository string, prefix string, data []byte) string {
	shaBytes := sha256.Sum256(data)
	hexSha := hex.EncodeToString(shaBytes[:])
	digest := fmt.Sprintf("sha256:%s", hexSha)

	// Define a link from the prefix-ed SHA to the SHA itself.
	d.addDigestLink(repository, prefix, digest)

	// Define the actual data.
	dataPath := fmt.Sprintf(
		"/docker/registry/v2/blobs/sha256/%s/%s/data",
		hexSha[0:2],
		hexSha)

	d.contentPaths[dataPath] = data
	return digest
}

func (d *localServeDriver) Name() string {
	return "localserve"
}

func (d *localServeDriver) GetContent(ctx context.Context, path string) ([]byte, error) {
	if contentBytes, found := d.contentPaths[path]; found {
		return contentBytes, nil
	}

	return nil, fmt.Errorf("Unknown file")
}

func (d *localServeDriver) PutContent(ctx context.Context, subPath string, contents []byte) error {
	panic("Not supported")
}

func (d *localServeDriver) ReadStream(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	contentLocation, found := d.externalContentPaths[path]
	if !found {
		return nil, fmt.Errorf("Unknown file")
	}

	file, err := os.OpenFile(contentLocation, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, storagedriver.PathNotFoundError{Path: path}
		}

		return nil, err
	}

	seekPos, err := file.Seek(int64(offset), os.SEEK_SET)
	if err != nil {
		file.Close()
		return nil, err
	} else if seekPos < int64(offset) {
		file.Close()
		return nil, storagedriver.InvalidOffsetError{Path: path, Offset: offset}
	}

	return file, nil
}

func (d *localServeDriver) WriteStream(ctx context.Context, subPath string, offset int64, reader io.Reader) (nn int64, err error) {
	panic("Not supported")
}

func (d *localServeDriver) Stat(ctx context.Context, subPath string) (storagedriver.FileInfo, error) {
	if contentBytes, found := d.contentPaths[subPath]; found {
		return fileInfo{subPath, int64(len(contentBytes))}, nil
	}

	if contentLocation, found := d.externalContentPaths[subPath]; found {
		contentFile, err := os.Open(contentLocation)
		if err != nil {
			return fileInfo{}, err
		}

		defer contentFile.Close()
		stat, err := contentFile.Stat()
		if err != nil {
			return fileInfo{}, err
		}

		return fileInfo{subPath, stat.Size()}, nil
	}

	return nil, fmt.Errorf("Unknown file")
}

func (d *localServeDriver) List(ctx context.Context, subPath string) ([]string, error) {
	panic("Not supported")

}

func (d *localServeDriver) Move(ctx context.Context, sourcePath string, destPath string) error {
	panic("Not supported")

}

func (d *localServeDriver) Delete(ctx context.Context, subPath string) error {
	panic("Not supported")
}

func (d *localServeDriver) URLFor(ctx context.Context, path string, options map[string]interface{}) (string, error) {
	return "", storagedriver.ErrUnsupportedMethod{}
}

type fileInfo struct {
	path string
	size int64
}

func (i fileInfo) Path() string {
	return i.path
}

func (i fileInfo) Size() int64 {
	return i.size
}

func (i fileInfo) ModTime() time.Time {
	return time.Now()
}

func (i fileInfo) IsDir() bool {
	return false
}
