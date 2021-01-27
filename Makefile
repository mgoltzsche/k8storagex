# Current Operator version
VERSION ?= 0.0.1
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Image URL to use all building/pushing image targets
IMG ?= mgoltzsche/cache-manager:0.0.1-local
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

MANAGER_BUILD_TAGS ?=

KUBE_CACHE_IMG ?= mgoltzsche/kube-cache:$(VERSION)
KUBE_CACHE_BUILD_TAGS ?= exclude_graphdriver_devicemapper exclude_graphdriver_btrfs btrfs_noversion containers_image_ostree_stub containers_image_openpgp

BUILD_TAGS ?= $(KUBE_CACHE_BUILD_TAGS) $(MANAGER_BUILD_TAGS)

all: manager kube-cache

static-manifests: manifests
	kpt fn run --network config

deploy:
	kpt live apply config/static

undeploy:
	kpt live destroy config/static

install-buildah: docker-build
	CID=`docker create $(IMAGE)` && \
	docker cp $$CID:/usr/bin/buildah /usr/local/bin/buildah; \
	STATUS=$$?; \
	docker rm $$CID; \
	exit $$STATUS

# Run tests
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate fmt vet manifests
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test -tags "$(BUILD_TAGS)" ./... -coverprofile cover.out

# Build dcachefs binary
kube-cache: fmt vet
	go build -o bin/kube-cache -tags "$(BUILD_TAGS)" ./cmd/kube-cache

kube-cache-image:
	docker build -t $(KUBE_CACHE_IMG) -f Dockerfile-storage .
	#docker build -t $(KUBE_CACHE_IMG) helper

test-kube-cache: kube-cache-image
	IMAGE=${KUBE_CACHE_IMG} ./e2e/run-tests.sh

clean:
	docker run --rm --privileged -v `pwd`:/data alpine:3.12 /bin/sh -c ' \
		umount /data/testmount/*; \
		umount /data/testmount/.cache/overlay; \
		umount /data/testmount/.cache/aufs; \
		rm -rf /data/testmount'

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager ./cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./cmd/...
	go fmt ./internal/...

# Run go vet against code
vet:
	go vet -tags "$(BUILD_TAGS)" ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build:
	docker build -t ${IMG} .

# Push the docker image
docker-push:
	docker push ${IMG}

kind-load-images: kube-cache-image docker-build
	kind load docker-image ${IMG}
	kind load docker-image ${KUBE_CACHE_IMG}

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .
