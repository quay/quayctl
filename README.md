// TODO(quayteam): implement testpull

# Build

You need Docker and a working Go environment ($GOPATH, $GOROOT, ...).

```
make [all | darwin-x64 | linux-x86 | linux-x64 | linux-arm | windows-x86 | windows-x64 ]
```

It produces testpull's binaries, compiled with Go 1.6, in `build/$PLATFORM/quayctl` and the corresponding SHA1 sums in `build/$PLATFORM/quayctl.sha`.
