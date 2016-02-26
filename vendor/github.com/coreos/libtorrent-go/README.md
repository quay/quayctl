libtorrent-go [![Build Status](https://travis-ci.org/coreos/libtorrent-go.svg?branch=master)](https://travis-ci.org/coreos/libtorrent-go)
=============

Cross-compiled SWIG Go bindings for libtorrent-rasterbar 1.0.8 using Go 1.6.

## Supported platforms

-	android-arm
-	android-x64
-	android-x86
-	darwin-x64
-	linux-arm
-	linux-x64
-	linux-x86
-	windows-x64
-	windows-x86

## How to use it?

+ First, you need a working Go project and [Docker](https://docs.docker.com/engine/installation/)

+ Download libtorrent-go:
```
go get github.com/coreos/libtorrent-go
```

+ Build libtorrent-go:
```
cd $GOPATH/go/src/github.com/coreos/libtorrent-go
make [all | android-arm | darwin-x64 | linux-x86 | linux-x64 | linux-arm | windows-x86 | windows-x64 ]
```
The cross-compilation is done within Docker containers, which are based on [github.com/coreos/cross-compiler](https://github.com/coreos/cross-compiler) and on which the libtorrent-rasterbar and Go are compiled. The container images are pulled from [Quay.io](https://quay.io/repository/coreos/libtorrent-go). You may also rebuild the images by yourself by running:
```
make env
```
Again, this is totally optionnal and depends on your needs. Note that this could take a long-time. You may specify the `PLATFORMS` variable in order to build a subset of the containers.

+ Import libtorrent-go in your Go project:
```
import "github.com/coreos/libtorrent-go"
```

Built packages will be placed as `$GOPATH/pkg/<platform>/libtorrent-go.a`

## Why another fork?

Forked from <https://github.com/steeve/libtorrent-go>

+ Go 1.6
+ CamelCased identifier names
+ Simplified build steps
+ peer_info support
+ Save and load resume_data support
+ Android ARM fixed

#### Acknowledgements:
- [steeve](https://github.com/steeve) for his awesome work.
- [dimitriss](https://github.com/dimitriss) for his great updates.
- [scakemyer](https://github.com/scakemyer) for his Go 1.6 updates.
