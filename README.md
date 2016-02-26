# quayctl

[![Build Status](https://api.travis-ci.org/coreos/quayctl.svg?branch=master "Build Status")](https://travis-ci.org/coreos/quayctl)
[![Docker Repository on Quay](https://quay.io/repository/coreos/quayctl/status "Docker Repository on Quay")](https://quay.io/repository/coreos/quayctl)
[![Go Report Card](https://goreportcard.com/badge/coreos/quayctl "Go Report Card")](https://goreportcard.com/report/coreos/quayctl)
[![GoDoc](https://godoc.org/github.com/coreos/quayctl?status.svg "GoDoc")](https://godoc.org/github.com/coreos/quayctl)
[![IRC Channel](https://img.shields.io/badge/freenode-%23quay-blue.svg "IRC Channel")](http://webchat.freenode.net/?channels=quay)

quayctl is an open source command-line client for [Quay].

Features include:

- Ability to pull docker images via [BitTorrent]
- Ability to pull squashed docker images

[Quay]: https://quay.io
[BitTorrent]: https://en.wikipedia.org/wiki/BitTorrent

## Getting Started

### Compiling From Source

To build quayctl, you need to latest stable version of [Docker], [Go 1.6] and a working [Go environment].

[Docker]: https://github.com/docker/docker/releases
[Go]: https://github.com/golang/go/releases
[Go environment]: https://golang.org/doc/code.html

```
$ go get github.com/coreos/quayctl
$ cd $GOPATH/github.com/coreos/quayctl
$ export PLATFORM='all | darwin-x64 | linux-x86 | linux-x64 | linux-arm | windows-x86 | windows-x64'
$ make $PLATFORM
```

`make` will produce quayctl binaries in `$GOPATH/build/$PLATFORM/quayctl` and the corresponding SHA1 sums in `$GOPATHbuild/$PLATFORM/quayctl.sha`.

## Related Links

- [Quay](https://quay.io)
- [Quay Docs](https://docs.quay.io)
