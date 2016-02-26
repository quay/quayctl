ifeq ($(OS), Windows_NT)
    HOST_OS = windows
    ifeq ($(PROCESSOR_ARCHITECTURE), AMD64)
        ARCH = x64
    else ifeq ($(PROCESSOR_ARCHITECTURE), x86)
        ARCH = x86
    endif
else
    UNAME_S := $(shell uname -s)
    UNAME_M := $(shell uname -m)
    ifeq ($(UNAME_S), Linux)
        HOST_OS = linux
    else ifeq ($(UNAME_S), Darwin)
        HOST_OS = darwin
    endif
    ifeq ($(UNAME_M), x86_64)
        HOST_ARCH = x64
    else ifneq ($(filter %86, $(UNAME_M)),)
        HOST_ARCH = x86
    else ifneq ($(findstring arm, $(UNAME_M)),)
        HOST_ARCH = arm
    endif
endif

ifneq ($(CROSS_TRIPLE),)
	CC := $(CROSS_TRIPLE)-$(CC)
	CXX := $(CROSS_TRIPLE)-$(CXX)
	STRIP := $(CROSS_TRIPLE)-strip
endif

GCC_TARGET = $(CC)

ifneq ($(findstring darwin, $(GCC_TARGET)),)
    TARGET_OS = darwin
else ifneq ($(findstring mingw, $(GCC_TARGET)),)
    TARGET_OS = windows
else ifneq ($(findstring android, $(GCC_TARGET)),)
    TARGET_OS = android
else ifneq ($(findstring linux, $(GCC_TARGET)),)
    TARGET_OS = linux
endif

ifneq ($(findstring x86_64, $(GCC_TARGET)),)
    TARGET_ARCH = x64
else ifneq ($(findstring i386, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i486, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i586, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring i686, $(GCC_TARGET)),)
    TARGET_ARCH = x86
else ifneq ($(findstring arm, $(GCC_TARGET)),)
    TARGET_ARCH = arm
endif

ifeq ($(TARGET_ARCH),x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH),x64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH),arm)
	GOARCH = arm
	GOARM = 6
endif

ifeq ($(TARGET_OS), windows)
	EXT = .exe
	GOOS = windows
else ifeq ($(TARGET_OS), darwin)
	EXT =
	GOOS = darwin
	# Needs this or cgo will try to link with libgcc, which will fail
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
	GO_LDFLAGS = -linkmode=external -extld=$(CC)
else ifeq ($(TARGET_OS), linux)
	EXT =
	GOOS = linux
	GO_LDFLAGS = -linkmode=external -extld=$(CC)
else ifeq ($(TARGET_OS), android)
	EXT =
	GOOS = android
	ifeq ($(TARGET_ARCH), arm)
		GOARM = 7
	else
		GOARM =
	endif
	GO_LDFLAGS = -linkmode=external -extldflags=-pie -extld=$(CC)
endif
