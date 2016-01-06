// TODO(quayteam): implement testpull

# Build

In order to build, you need a working Go 1.4.3 installation and Docker.
 
```
make build-envs
make alldist
```

By default, it will build for the following platforms:
- darwin-x64
- windows-x86
- windows-x64
- linux-x86
- linux-x64
- linux-arm

It is possible to build against a subset of these targets by defining the `PLATFORMS` env variable.
