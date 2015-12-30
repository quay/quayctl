// manifest packages provides helper methods for retrieving and parsing a Docker manifest from
// a remote repository.
package manifest

import (
	"log"

	"github.com/docker/docker/cliconfig"

	distlib "github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/docker/distribution"
	"github.com/docker/docker/reference"
	"github.com/docker/docker/registry"
	"github.com/docker/go-connections/tlsconfig"

	"golang.org/x/net/context"
)

// getRepositoryClient returns a client for performing registry operations against the given named
// image.
func getRepositoryClient(image reference.Named, scopes ...string) (distlib.Repository, error) {
	// Lookup the index information for the name.
	indexInfo, err := registry.ParseSearchIndexInfo(image.String())
	if err != nil {
		return nil, err
	}

	// Retrieve the user's Docker configuration file (if any).
	configFile, err := cliconfig.Load(cliconfig.ConfigDir())
	if err != nil {
		return nil, err
	}

	// Resolve the authentication information for the registry specified, via the config file.
	authConfig := registry.ResolveAuthConfig(configFile.AuthConfigs, indexInfo)

	repoInfo := &registry.RepositoryInfo{
		image,
		indexInfo,
		false,
	}

	metaHeaders := map[string][]string{}
	tlsConfig := tlsconfig.ServerDefault

	endpoint := registry.APIEndpoint{
		URL:          "https://" + image.Hostname(),
		Version:      registry.APIVersion2,
		Official:     false,
		TrimHostname: true,
		TLSConfig:    &tlsConfig,
	}

	log.Printf("Retrieving Docker client for image %v", image)
	ctx := context.Background()
	repo, _, err := distribution.NewV2Repository(ctx, repoInfo, endpoint, metaHeaders, &authConfig, scopes...)
	return repo, err
}

// getTagOrDigest returns the tag or digest for the given image.
func getTagOrDigest(image reference.Named) string {
	if withTag, ok := image.(reference.NamedTagged); ok {
		return withTag.Tag()
	} else if withDigest, ok := image.(reference.Canonical); ok {
		return withDigest.Digest().String()
	}

	return "latest"
}

// Downloads the manifest for the given image, using the given credentials.
func Download(image string) (*schema1.SignedManifest, error) {
	// Parse the image name as a docker image reference.
	named, err := reference.ParseNamed(image)
	if err != nil {
		return nil, err
	}

	// Create a reference to a repository client for the repo.
	repo, err := getRepositoryClient(named, "pull")
	if err != nil {
		return nil, err
	}

	// Retrieve the manifest for the tag.
	log.Printf("Downloading manifest for image %v", image)
	ctx := context.Background()
	manSvc, err := repo.Manifests(ctx)
	if err != nil {
		return nil, err
	}

	unverifiedManifest, err := manSvc.GetByTag(getTagOrDigest(named))
	if err != nil {
		return nil, err
	}

	_, verr := schema1.Verify(unverifiedManifest)
	if verr != nil {
		return nil, verr
	}

	return unverifiedManifest, nil
}
