# Simple Makefile wrapper for tools/build.go

SHELL := /bin/bash

# Cache directory redirection (use absolute paths)
CACHE_DIR ?= $(abspath local/.gocache)
export GOCACHE := $(CACHE_DIR)/go-build
export GOMODCACHE := $(CACHE_DIR)/go-mod
export GOTMPDIR := $(CACHE_DIR)/tmp
export XDG_CACHE_HOME := $(CACHE_DIR)

# Common build variables
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

BUILD_GO := tools/build.go
GO_RUN := go run $(BUILD_GO)

.PHONY: all ensure-cache build release install run test test-verbose proto

all: build

ensure-cache:
	@mkdir -p "$(GOCACHE)" "$(GOMODCACHE)" "$(GOTMPDIR)"

build: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH)

release: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH) -release

install: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH) -install

# Run TinyRange after building; pass ARGS='...' to forward arguments
run: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH) -run -- $(ARGS)

# Run tests; set TEST to file/dir (default: tests)
TEST ?= tests
test: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH) -test=$(TEST)

test-verbose: ensure-cache
	$(GO_RUN) -os=$(OS) -arch=$(ARCH) -test=$(TEST) -test-verbose

# Generate protobufs
proto: ensure-cache
	$(GO_RUN) -proto

fmt:
	go fmt ./...

vet:
	go vet ./...

ci:
	golangci-lint run