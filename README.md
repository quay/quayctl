# quayctl

[![Build Status](https://api.travis-ci.org/coreos/quayctl.svg?branch=master "Build Status")](https://travis-ci.org/coreos/quayctl)
[![Go Report Card](https://goreportcard.com/badge/coreos/quayctl "Go Report Card")](https://goreportcard.com/report/coreos/quayctl)
[![GoDoc](https://godoc.org/github.com/coreos/quayctl?status.svg "GoDoc")](https://godoc.org/github.com/coreos/quayctl)
[![IRC Channel](https://img.shields.io/badge/freenode-%23quay-blue.svg "IRC Channel")](http://webchat.freenode.net/?channels=quay)

quayctl is an open source command-line client for interacting with [Quay].

Current Features:

- Ability to pull ACI images into rkt via [BitTorrent]
- Ability to pull docker images via [BitTorrent]
- Ability to pull squashed docker images via [BitTorrent]

[Quay]: https://quay.io
[BitTorrent]: https://en.wikipedia.org/wiki/BitTorrent

## Downloading

Pre-built binaries for various distributions can be found on the [Releases] page.

[Releases]: https://github.com/coreos/quayctl/releases

## Getting Started

### Using BitTorrent for pulling images

quayctl can be used to perform a `rkt fetch` or `docker pull`-like operation against supported registries[1] with image data being downloaded
via the [BitTorrent] protocol.

Pulling via BitTorrent (and subsequently seeding) can lead to significant performance boosts and predictability in
regions outside of Quay.io's storage center in AWS US-East [2]

When used in the Enterprise setting, a set of bastion hosts can be set to pull the image from an external registry, with
all production hosts only pulling from the seeding bastions.

[1]: Currently Quay.io and Quay Enterprise

[2]: ~30% improvement in test pulls from AWS Sydney for a 300MB squashed image

#### Fetching an image for rkt

An image can be fetched via quayctl by doing:

```
quayctl rkt torrent pull quay.io/yournamespace/yourrepository:optionaltag
```

The image will be downloaded into the rkt image store and signature validated.

**Note:** In order for a rkt image to be available, it must be pulled *once* via a normal `rkt fetch` (from anywhere) before quayctl is run. This restriction will be removed in a followup release.


#### Pulling an image for Docker

An image can be pulled via quayctl by doing:

```
quayctl docker torrent pull quay.io/yournamespace/yourrepository:optionaltag
```

Each layer of the image will be downloaded, with automatic uploading to all other clients during the pull. Once complete, the image will be in the normal `docker images` list.

#### Private images

quayctl uses the stored container runtime credentials for its authorization.

For rkt, credentials can be set via [rkt configuration](https://coreos.com/rkt/docs/latest/configuration.html).

For Docker, credentials can be set by executing a normal `docker login` command before quayctl is executed.


#### Seeding an image

It is highly recommended for machines to seed images once they have performed the pull. Seeding can be enabled by executing:

```
quayctl rkt torrent seed quay.io/yournamespace/yourrepository:optionaltag
```

The command will block *indefinitely* while seeding.

##### Seed for a set period of time

To seed for a set period of time, after which the binary will terminate, add the `--duration` flag:

```
quayctl docker torrent seed quay.io/yournamespace/yourrepository:optionaltag --duration 10m
```


#### Squashed images

quayctl can be used to pull a **squashed** version of a Docker image via BitTorrent.

**Note:** In order for a squashed image to be available, it must be pulled *once* via `curl` (from anywhere) before quayctl is run. This restriction will be removed in a followup release.

```
quayctl docker torrent pull quay.io/yournamespace/yourrepository:optionaltag --squashed
```


#### Skipping the web seed

If quayctl is used on machines without access to the registry, adding the flag `--skip-web-seed` will force the torrent
to only pull from seeding peers, rather than attempting to use the web seed from the registry's storage engine.


## Frequently Asked Questions/Issues

### Where does using BitTorrent for pulling images help?

BitTorrent is useful in any environment that will be pulling large images multiple times. For example, a cluster
of machines under VPC would receive benefit by being able to share image data amongst
themselves on the internal network, rather than having to all use the same VPCed network link to Quay's external storage.

Another example would be any machines running behind a firewall without access to the internet as a whole. Such machines could
be configured to skip the webseed (to Quay's storage) and, instead, pull all their layer data from peers on specialized machines that
do have access to the external network.


### I receive a 403 when trying to download a private rkt image

Please make sure to setup rkt credentials via [rkt configuration](https://coreos.com/rkt/docs/latest/configuration.html) that have access to the image.


### I receive a 403 when trying to download a private Docker image

Please perform a normal `docker login` with your credentials before your perform the pull. quayctl reads the credentials from
the same source as the Docker CLI:

```
docker login quay.io
Username: myprivate
Password: *********
Docker Login successful

quayctl torrent pull quay.io/myprivate/imagehere
Downloading manifest for image quay.io/myprivate/imagehere...
```

### I receive a 404 error when trying to pull an image via rkt

In order for Quay to serve the torrent for an ACI image, the image must have been previously
computed and cached. ACI images are not created until they are first requested, so a fetch via
`rkt fetch` is required before `torrent pull --squashed`
can be used. We are currently working on removing of this restriction.

### I receive a 404 error when trying to pull a squashed image

In order for Quay to serve the torrent for a squashed image, the image must have been previously computed and cached. Squashed images are not created until
they are first requested, so a download of the squashed image via the normal `curl` method is required before `torrent pull --squashed`
can be used. We are currently working on removing of this restriction.

### I want to use my own torrent tracker(s)

The tracker(s) used can be overridden via the `--tracker` flag:

```
quayctl rkt torrent pull quay.io/myprivate/repository --tracker mycooltracker.something.com
```


## Compiling From Source

To build quayctl, you will need the latest stable version of [Docker], [Go 1.6] and a working [Go environment].
quayctl uses [libtorrent rasterbar] via SWIG and thus has a `Makefile` that compiles the object file for libtorrent for the target OS.

To compile:

```
$ export PLATFORM='darwin-x64'
$ go get -d github.com/coreos/quayctl
$ cd $GOPATH/src/github.com/coreos/quayctl
$ make $PLATFORM
```

`make` will produce quayctl binaries in `$GOPATH/build/$PLATFORM/quayctl` and the corresponding SHA1 sums in `$GOPATHbuild/$PLATFORM/quayctl.sha`.

[Docker]: https://github.com/docker/docker/releases
[Go 1.6]: https://github.com/golang/go/releases
[Go environment]: https://golang.org/doc/code.html
[libtorrent rasterbar]: http://www.libtorrent.org/

#### Supported Platforms

| Platform    | Supported |
|:-----------:|:---------:|
| all         |     X     |
| darwin-x64  |     ✓     |
| linux-x86   |     ✓     |
| linux-x64   |     ✓     |
| linux-arm   |     ✓     |
| windows-x86 |     X     |
| windows-x64 |     X     |

## Related Links

- [Quay](https://quay.io)
- [Quay Docs](https://docs.quay.io)
