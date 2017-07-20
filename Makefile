DOCKER_REGISTRY   ?= gcr.io
IMAGE_PREFIX      ?= kubernetes-helm-rudder-federation
SHORT_NAME        ?= rudder
DIST_DIRS         = find * -type d -exec
APP               = rudder
PACKAGE           = github.com/kubernetes-helm/rudder-federation
IMAGE_REPO        = nebril/rudder-fed

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

# dind options
K8S_CLUSTER_MARKER = .k8s-cluster
K8S_VERSION ?= v1.5
HELM_BINARY_PATH ?= /tmp/


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
docker-build: docker-binary
	docker build --rm -t $(IMAGE_REPO) rootfs

.PHONY: clean
clean:
	@rm -rf $(BINDIR) ./rootfs/rudder
	-rm e2e.test

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


.PHONY: img-in-dind
img-in-dind: docker-build $(K8S_CLUSTER_MARKER)
	IMAGE_REPO=$(IMAGE_REPO) ./scripts/import.sh

.PHONY: e2e
e2e: $(K8S_CLUSTER_MARKER) img-in-dind prepare-helm
	go test -c -o e2e.test ./e2e/
	PATH=$(PATH):$(HELM_BINARY_PATH)/linux-amd64 ./e2e.test --cluster-url=http://0.0.0.0:8080

.PHONY: clean-all
clean-all: clean clean-k8s

.PHONY: clean-k8s
clean-k8s:
	./kubeadm-dind-cluster/fixed/dind-cluster-$(K8S_VERSION).sh clean
	-rm $(K8S_CLUSTER_MARKER)

$(K8S_CLUSTER_MARKER):
	if [ ! -d "kubeadm-dind-cluster" ]; then git clone https://github.com/Mirantis/kubeadm-dind-cluster.git; fi
	./kubeadm-dind-cluster/fixed/dind-cluster-$(K8S_VERSION).sh up
	touch $(K8S_CLUSTER_MARKER)

.PHONE: prepare-helm
prepare-helm:
	pushd $(HELM_BINARY_PATH) \
	&& curl https://kubernetes-helm.storage.googleapis.com/helm-v2.3.0-linux-amd64.tar.gz > helm.tar.gz \
	&& tar xf helm.tar.gz && popd
