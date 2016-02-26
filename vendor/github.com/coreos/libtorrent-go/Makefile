# Name of the project.
NAME = libtorrent-go
GO_PACKAGE = github.com/coreos/$(NAME)
DOCKER_IMAGE = quay.io/coreos/$(NAME)

# Set binaries and platform specific variables.
CC = cc
CXX = c++
PKG_CONFIG = pkg-config
DOCKER = docker

# Platforms on which we want to build the project.
PLATFORMS = \
	android-arm \
	android-x64 \
	android-x86 \
	darwin-x64 \
	linux-arm \
	linux-x64 \
	linux-x86 \
	windows-x64 \
	windows-x86

include platform_host.mk

ifneq ($(CROSS_TRIPLE),)
	CC := $(CROSS_TRIPLE)-$(CC)
	CXX := $(CROSS_TRIPLE)-$(CXX)
endif

include platform_target.mk

ifeq ($(TARGET_ARCH), x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH), x64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH), arm)
	GOARCH = arm
	GOARM = 6
endif

ifeq ($(TARGET_OS), windows)
	GOOS = windows
else ifeq ($(TARGET_OS), darwin)
	GOOS = darwin
else ifeq ($(TARGET_OS), linux)
	GOOS = linux
else ifeq ($(TARGET_OS), android)
	GOOS = android
	ifeq ($(TARGET_ARCH), arm)
		GOARM = 7
	else
		GOARM =
	endif
	GO_LDFLAGS = -extldflags=-pie
endif

ifneq ($(CROSS_ROOT),)
	CROSS_CFLAGS = -I$(CROSS_ROOT)/include -I$(CROSS_ROOT)/$(CROSS_TRIPLE)/include
	CROSS_LDFLAGS = -L$(CROSS_ROOT)/lib
	PKG_CONFIG_PATH = $(CROSS_ROOT)/lib/pkgconfig
endif

LIBTORRENT_CFLAGS = $(CFLAGS) $(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) $(PKG_CONFIG) --cflags libtorrent-rasterbar)
LIBTORRENT_LDFLAGS = $(LDFLAGS) $(shell PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) $(PKG_CONFIG) --static --libs libtorrent-rasterbar)
DEFINE_IGNORES = __STDC__|_cdecl|__cdecl|_fastcall|__fastcall|_stdcall|__stdcall|__declspec
CC_DEFINES = $(shell echo | $(CC) -dM -E - | grep -v -E "$(DEFINE_IGNORES)" | sed -E "s/\#define[[:space:]]+([a-zA-Z0-9_()]+)[[:space:]]+(.*)/-D\1="\2"/g" | tr '\n' ' ')

ifeq ($(TARGET_OS), windows)
	CC_DEFINES += -DSWIGWIN
	CC_DEFINES += -D_WIN32_WINNT=0x0501
	ifeq ($(TARGET_ARCH), x64)
		CC_DEFINES += -DSWIGWORDSIZE32
	endif
else ifeq ($(TARGET_OS), darwin)
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
	CC_DEFINES += -DSWIGMAC
	CC_DEFINES += -DBOOST_HAS_PTHREADS
endif

OUT_PATH = $(shell go env GOPATH)/pkg/$(GOOS)_$(GOARCH)
OUT_LIBRARY = $(OUT_PATH)/$(GO_PACKAGE).a

.PHONY: $(PLATFORMS)

env:
	for i in $(PLATFORMS); do \
		$(DOCKER) build -t $(DOCKER_IMAGE):$$i $$i; \
	done

push-env: env
	for i in $(PLATFORMS); do \
		$(DOCKER) push $(DOCKER_IMAGE):$$i; \
	done

all:
	for i in $(PLATFORMS); do \
		$(MAKE) $$i; \
	done

$(PLATFORMS):
	$(DOCKER) run --rm -v $(GOPATH):/go -v $(shell pwd):/go/src/$(GO_PACKAGE) -w /go/src/$(GO_PACKAGE) -e GOPATH=/go $(DOCKER_IMAGE):$@ make re;

build:
	SWIG_FLAGS='$(CC_DEFINES) $(LIBTORRENT_CFLAGS)' \
	CC=$(CC) CXX=$(CXX) \
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) \
	CGO_ENABLED=1 \
	GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
	PATH=.:$$PATH \
	go install -v -ldflags '$(GO_LDFLAGS)' -x

clean:
	rm -rf $(OUT_LIBRARY)

re: clean build
