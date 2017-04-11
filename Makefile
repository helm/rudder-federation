DOCKER_REGISTRY   ?= gcr.io
IMAGE_PREFIX      ?= kubernetes-helm-rudder-appcontroller
SHORT_NAME        ?= rudder
DIST_DIRS         = find * -type d -exec
APP               = rudder
PACKAGE           = github.com/nebril/rudder-appcontroller

# go option
GO        ?= go
PKG       := $(shell glide novendor)
TAGS      :=
TESTS     := .
TESTFLAGS :=
LDFLAGS   :=
GOFLAGS   :=
BINDIR    := $(CURDIR)/bin
BINARIES  := rudder

# Required for globs to work correctly
SHELL=/bin/bash

.PHONY: all
all: build

.PHONY: build
build:
	GOBIN=$(BINDIR) $(GO) install $(GOFLAGS) -tags '$(TAGS)' -ldflags '$(LDFLAGS)' ${PACKAGE}/cmd/...

.PHONY: docker-binary
docker-binary: BINDIR = ./rootfs
docker-binary: GOFLAGS += -a -installsuffix cgo
docker-binary:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -o $(BINDIR)/rudder $(GOFLAGS) -tags '$(TAGS)' -ldflags '$(LDFLAGS)' ${PACKAGE}/cmd/rudder

.PHONY: docker-build
docker-build: check-docker docker-binary
	docker build --rm -t ${IMAGE} rootfs
	docker tag ${IMAGE} ${MUTABLE_IMAGE}

.PHONY: clean
clean:
	@rm -rf $(BINDIR) ./rootfs/rudder

HAS_GLIDE := $(shell command -v glide;)
HAS_GOX := $(shell command -v gox;)
HAS_GIT := $(shell command -v git;)

.PHONY: bootstrap
bootstrap:
ifndef HAS_GLIDE
	go get -u github.com/Masterminds/glide
endif
ifndef HAS_GOX
	go get -u github.com/mitchellh/gox
endif

ifndef HAS_GIT
	$(error You must install Git)
endif
	glide install --strip-vendor
	go build -o bin/protoc-gen-go ./vendor/github.com/golang/protobuf/protoc-gen-go

include versioning.mk
