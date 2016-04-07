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

package dockerclient

import (
	"fmt"

	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/reference"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
)

// localServeDriverFactory defines a factory for constructing a Docker Registry-compatible
// storage engine that serves the given layer information.
type localServeDriverFactory struct {
	image      reference.Named
	manifest   *schema1.SignedManifest
	layerPaths map[string]string
}

func (factory *localServeDriverFactory) Create(parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	// Determine the current tag.
	var tagName = "latest"
	if tagged, ok := factory.image.(reference.NamedTagged); ok {
		tagName = tagged.Tag()
	}

	driver := &localServeDriver{
		contentPaths:         map[string][]byte{},
		externalContentPaths: map[string]string{},
	}

	// Add the manifest as a linked file.
	manifestJson, _ := factory.manifest.MarshalJSON()
	digest := driver.addLinkedData(factory.image.RemoteName(), "_manifests/revisions", manifestJson)

	// Add a link from the tag to the manifest.
	driver.addLink(factory.image.RemoteName(),
		fmt.Sprintf("_manifests/tags/%s/current/link", tagName),
		digest)

	// Add each blob layer.
	for blobDigest, blobLocation := range factory.layerPaths {
		driver.addLinkedFile(factory.image.RemoteName(), "_layers", blobDigest, blobLocation)
	}

	return driver, nil
}
