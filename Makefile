# Name of the project.
NAME = quayctl
PROJECT = github.com/coreos/quayctl
PKG = github.com/coreos/quayctl/cmd/quayctl

# Platforms on which we want to build the project.
PLATFORMS = \
	darwin-x64 \
	linux-arm \
	linux-x64 \
	linux-x86 \
	windows-x64 \
	windows-x86
	# android-arm \
	# android-x64 \
	# android-x86 \

# Path to the libtorrent-go package and Docker image.
LIBTORRENT_GO = github.com/coreos/libtorrent-go
LIBTORRENT_GO_DOCKER_IMAGE = quay.io/coreos/libtorrent-go

# Set binaries and platform specific variables.
CC = cc
CXX = c++
DATE = date
STRIP = strip
SHASUM = shasum
GO = go
GODEP = godep
GIT = git
DOCKER = docker
UPX = upx
RM = rm

include platform.mk

# Additional tags and LDFLAGS to use during the compilation.
GITHASH = $(shell $(GIT) rev-parse HEAD)
BUILDTIME = $(shell $(DATE) -u +%Y-%m-%d_%I:%M:%S%p)
GO_LDFLAGS += -w -X "main.githash=$(GITHASH)" -X "main.buildtime=$(BUILDTIME)"
GO_GCFLAGS +=
GO_BUILD_TAGS = netgo std
LDFLAGS =

OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)

.PHONY: $(PLATFORMS) build clean

# Build every supported platforms.
all:
	for i in $(PLATFORMS); do \
		$(MAKE) $$i; \
	done

# Launch a single platform build using `make $(NAME)` inside a platform specific Docker container.
$(PLATFORMS): TARGET_OS=$$(echo $@ | cut -f1 -d-)
$(PLATFORMS): TARGET_ARCH=$$(echo $@ | cut -f2 -d-)
$(PLATFORMS):
	$(DOCKER) run --rm \
		-v $(GOPATH):/go \
		-e GOPATH=/go \
		-w /go/src/$(PROJECT) \
		$(LIBTORRENT_GO_DOCKER_IMAGE):$@ \
		make $(NAME) TARGET_OS=$(TARGET_OS) TARGET_ARCH=$(TARGET_ARCH) GITHASH=$(GITHASH) BUILDTIME=$(BUILDTIME)

# Build libtorrent-go for the specified platform.
$(GOPATH)/pkg/%/$(LIBTORRENT_GO).a:
	$(MAKE) -C $(GOPATH)/src/$(LIBTORRENT_GO) $(PLATFORM)

# Called inside a platform specific Docker image,
# Delegate the app building using the target `$(BUILD_PATH)/$(OUTPUT_NAME)`,
# Does the vendoring, strip, upx and shasum as necesary.
$(NAME): $(BUILD_PATH)/$(OUTPUT_NAME)
ifeq ($(TARGET_OS), windows)
	find $(GOPATH)/pkg/$(GOOS)_$(GOARCH) -name *.dll -exec cp -f {} $(BUILD_PATH) \;
endif

ifeq ($(TARGET_OS), android)
	cp $(CROSS_ROOT)/$(CROSS_TRIPLE)/lib/libgnustl_shared.so $(BUILD_PATH)
	chmod +rx $(BUILD_PATH)/libgnustl_shared.so
endif

	@find $(BUILD_PATH) -type f ! -name "*.exe" -a ! -name "*.so" ! -name "*.sha" -exec $(STRIP) {} \;

ifneq ($(TARGET_ARCH), arm)
	@find $(BUILD_PATH) -type f ! -name "*.exe" -a ! -name "*.sha" -exec $(UPX) --lzma {} \;
endif

	$(SHASUM) -b $(BUILD_PATH)/$(OUTPUT_NAME) | cut -d' ' -f1 > $(BUILD_PATH)/$(OUTPUT_NAME).sha

# Create the output build folder.
$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

# Build the app.
$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH)
	LDFLAGS='$(LDFLAGS)' \
	CC=$(CC) CXX=$(CXX) \
	GOOS=$(GOOS) \
	GOARCH=$(GOARCH) \
	GOARM=$(GOARM) \
	CGO_ENABLED=1 \
	$(GO) build -v \
		-tags '$(GO_BUILD_TAGS)' \
		-gcflags '$(GO_GCFLAGS)' \
		-ldflags '$(GO_LDFLAGS)' \
		-o '$(BUILD_PATH)/$(OUTPUT_NAME)' \
		$(PKG)

# Remove output build folder.
clean:
	$(RM) -rf $(BUILD_PATH)
