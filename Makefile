#!/usr/bin/make -f

VERSION := $(shell echo $(shell git describe --tags) | sed 's/^v//')
COMMIT := $(shell git log -1 --format='%H')
GO_VERSION := $(shell cat go.mod | grep -E 'go [0-9].[0-9]+' | cut -d ' ' -f 2)
DOCKER := $(shell which docker)
BUILDDIR ?= $(CURDIR)/build

# Private module dependencies (e.g. the unreleased wasmvm/wasmd security forks).
GOPRIVATE ?= $(shell go env GOPRIVATE)
DOCKER_PRIVATE_ARGS := $(if $(GOPRIVATE),--build-arg GOPRIVATE=$(GOPRIVATE) --ssh default)

# build/ is also a directory; without .PHONY make treats "build" as up to date and skips it
.PHONY: build lint lint-fix lint-strict build-static build-release install clean

build: go.sum
ifeq ($(OS),Windows_NT)
	exit 1
else
	go build -mod=readonly $(BUILD_FLAGS) -o build/mantlemint ./sync.go
endif

lint:
	golangci-lint run --out-format=tab

lint-fix:
	golangci-lint run --fix --out-format=tab --issues-exit-code=0

lint-strict:
	find . -path './_build' -prune -o -type f -name '*.go' -exec gofumpt -w -l {} +

# Statically linked binary for the host architecture, extracted from the image.
build-static:
	mkdir -p $(BUILDDIR)
	$(DOCKER) buildx build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg GIT_VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		$(DOCKER_PRIVATE_ARGS) \
		--tag terramoney/mantlemint \
		--load \
		-f Dockerfile .
	$(DOCKER) rm -f temp || true
	$(DOCKER) create --name temp terramoney/mantlemint:latest
	$(DOCKER) cp temp:/usr/local/bin/mantlemint $(BUILDDIR)/
	$(DOCKER) rm -f temp

###############################################################################
###                                Release                                  ###
###############################################################################

# Portable, fully static release archives.
build-release: build-release-amd64 build-release-arm64

build-release-amd64: go.sum
	mkdir -p $(BUILDDIR)/release
	$(DOCKER) buildx create --name mantlemint-builder || true
	$(DOCKER) buildx use mantlemint-builder
	$(DOCKER) buildx build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg GIT_VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILDPLATFORM=linux/amd64 \
		--build-arg GOOS=linux \
		--build-arg GOARCH=amd64 \
		$(DOCKER_PRIVATE_ARGS) \
		-t mantlemint:local-amd64 \
		--load \
		-f Dockerfile .
	$(DOCKER) rm -f mantlemint-release || true
	$(DOCKER) create -ti --name mantlemint-release mantlemint:local-amd64
	$(DOCKER) cp mantlemint-release:/usr/local/bin/mantlemint $(BUILDDIR)/release/mantlemint
	tar -czvf $(BUILDDIR)/release/mantlemint_$(VERSION)_Linux_x86_64.tar.gz -C $(BUILDDIR)/release/ mantlemint
	rm $(BUILDDIR)/release/mantlemint
	$(DOCKER) rm -f mantlemint-release

build-release-arm64: go.sum
	mkdir -p $(BUILDDIR)/release
	$(DOCKER) buildx create --name mantlemint-builder || true
	$(DOCKER) buildx use mantlemint-builder
	$(DOCKER) buildx build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg GIT_VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILDPLATFORM=linux/arm64 \
		--build-arg GOOS=linux \
		--build-arg GOARCH=arm64 \
		$(DOCKER_PRIVATE_ARGS) \
		-t mantlemint:local-arm64 \
		--load \
		-f Dockerfile .
	$(DOCKER) rm -f mantlemint-release || true
	$(DOCKER) create -ti --name mantlemint-release mantlemint:local-arm64
	$(DOCKER) cp mantlemint-release:/usr/local/bin/mantlemint $(BUILDDIR)/release/mantlemint
	tar -czvf $(BUILDDIR)/release/mantlemint_$(VERSION)_Linux_arm64.tar.gz -C $(BUILDDIR)/release/ mantlemint
	rm $(BUILDDIR)/release/mantlemint
	$(DOCKER) rm -f mantlemint-release

install: go.sum
	go install -mod=readonly $(BUILD_FLAGS) ./

go.sum: go.mod
	@echo "--> Ensure dependencies have not been modified"
	@go mod verify

clean:
	rm -rf $(BUILDDIR)/

.PHONY: build build-static build-release build-release-amd64 build-release-arm64 install clean lint lint-fix lint-strict
