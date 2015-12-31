package dockerclient

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	"github.com/fsouza/go-dockerclient"
)

// DockerClient is an interface for all of the Docker™ interactions required of
// a worker.
type dockerClient interface {
	BuildImage(docker.BuildImageOptions) error
	PullImage(docker.PullImageOptions, docker.AuthConfiguration) error
	PushImage(docker.PushImageOptions, docker.AuthConfiguration) error
	TagImage(string, docker.TagImageOptions) error
	InspectImage(string) (*docker.Image, error)
	RemoveImage(string) error
}

func newDockerClient() (*docker.Client, error) {
	host, err := url.Parse(os.Getenv("DOCKER_HOST"))
	if err != nil {
		return nil, err
	}

	// Change to an https connection if we have a cert path.
	if os.Getenv("DOCKER_CERT_PATH") != "" {
		host.Scheme = "https"
	}

	c, err := docker.NewClient(host.String())
	if err != nil {
		return nil, err
	}

	// Set the client to use https.
	if os.Getenv("DOCKER_CERT_PATH") != "" {
		transport, err := buildTLSTransport(os.Getenv("DOCKER_CERT_PATH"))
		if err != nil {
			return nil, err
		}

		c.HTTPClient = &http.Client{Transport: transport}
	}

	return c, nil
}

func buildTLSTransport(basePath string) (*http.Transport, error) {
	roots := x509.NewCertPool()
	pemData, err := ioutil.ReadFile(basePath + "/ca.pem")
	if err != nil {
		return nil, err
	}

	// Add the certification to the pool.
	roots.AppendCertsFromPEM(pemData)

	// Create the certificate;
	crt, err := tls.LoadX509KeyPair(basePath+"/cert.pem", basePath+"/key.pem")
	if err != nil {
		return nil, err
	}

	// Create the new tls configuration using both the authority and certificate.
	conf := &tls.Config{
		RootCAs:      roots,
		Certificates: []tls.Certificate{crt},
	}

	// Create our own transport and return it.
	return &http.Transport{
		TLSClientConfig: conf,
	}, nil
}
