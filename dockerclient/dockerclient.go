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
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	"github.com/fsouza/go-dockerclient"
)

// HasImage returns true if the current Docker daemon reports that the image with the given
// ID exists.
func HasImage(imageId string) (bool, error) {
	client, err := newDockerClient()
	if err != nil {
		return false, err
	}

	found, err := client.InspectImage(imageId)
	if err != nil {
		return false, err
	}

	return found.ID == imageId, nil
}

func newDockerClient() (*docker.Client, error) {
	var dockerHost = os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	} else {
		host, err := url.Parse(dockerHost)
		if err != nil {
			return nil, err
		}

		// Change to an https connection if we have a cert path.
		if os.Getenv("DOCKER_CERT_PATH") != "" {
			host.Scheme = "https"
		}

		dockerHost = host.String()
	}

	c, err := docker.NewClient(dockerHost)
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
