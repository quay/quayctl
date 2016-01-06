// TODO(quayteam): implement testpull

# Build

First, you must restore the testpull's Go dependencies using Godeps:

```
godep restore
```

Then, you need to build the libtorrent-go's static library. It will create `libtorrent-go.a` for each support platform, in `$GOPATH/pkg/$PLATFORM/github.com/dmartinpro/`.

```
make libtorrent-go
```

Finally, you have to build testpull:

```
make alldist
```

It produces testpull's binaries, compiled with Go 1.4.3, in `build/$PLATFORM/testpull` and the corresponding SHA1 sums in `build/$PLATFORM/testpull.sha`.
When testpull's source code is updated, it is only necessary to re-run the last command.

By default, it will build for the following platforms:
- darwin-x64
- windows-x86
- windows-x64
- linux-x86
- linux-x64
- linux-arm

It is possible to build against a subset of these targets by defining the `PLATFORMS` variable, for instance:

```
make PLATFORMS=darwin-x64 libtorrent-go
make PLATFORMS=darwin-x64 alldist
```
