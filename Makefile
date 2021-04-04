# Current Operator version
VERSION ?= 0.0.0
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
IMG ?= mgoltzsche/cache-manager:$(VERSION)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

BUILD_DIR := $(shell pwd)/build
BIN_DIR := $(BUILD_DIR)/bin

BATS_DIR = $(BUILD_DIR)/tools/bats
BATS = $(BIN_DIR)/bats
KPT = $(BIN_DIR)/kpt

MANAGER_BUILD_TAGS ?=

DCOWFS_IMG ?= mgoltzsche/dcowfs:$(VERSION)
DCOWFS_BUILD_TAGS ?= exclude_graphdriver_devicemapper exclude_graphdriver_btrfs btrfs_noversion containers_image_ostree_stub containers_image_openpgp

BUILD_TAGS ?= $(DCOWFS_BUILD_TAGS) $(MANAGER_BUILD_TAGS)

STATIC_MANIFESTS = default minikube
DEPLOY_TARGETS = $(addprefix deploy-,$(STATIC_MANIFESTS))
UNDEPLOY_TARGETS = $(addprefix undeploy-,$(STATIC_MANIFESTS))


all: manager dcowfs

static-manifests: manifests
	kpt fn run --network config

$(DEPLOY_TARGETS): deploy-%:
	kpt live apply config/static/$*

$(UNDEPLOY_TARGETS): undeploy-%:
	kpt live destroy config/static/$*

install-buildah: legacy-helper-image
	CID=`docker create local/buildah-helper` && \
	docker cp $$CID:/usr/bin/buildah /usr/local/bin/buildah; \
	STATUS=$$?; \
	docker rm $$CID; \
	exit $$STATUS

legacy-helper-image:
	docker build -t local/buildah-helper -f helper/Dockerfile helper

# Run tests
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate fmt vet manifests
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test -tags "$(BUILD_TAGS)" ./... -coverprofile build/cover.out

# Build dcowfs binary
dcowfs: fmt vet
	go build -o build/bin/dcowfs -tags "$(BUILD_TAGS)" ./cmd/dcowfs

dcowfs-image:
	docker build -t $(DCOWFS_IMG) -f Dockerfile-dcowfs .

test-e2e: test-dcowfs test-operator

test-dcowfs: dcowfs-image $(BATS)
	export PATH="$(BIN_DIR):$$PATH"; \
	IMAGE=${DCOWFS_IMG} ./e2e/dcowfs/run-tests.sh

test-operator: $(BATS)
	@printf '\nRUNNING OPERATOR E2E TESTS...\n\n'
	kubectl wait --for condition=available --timeout 60s -n cache-storage deploy/cache-pvc-remover-controller-manager deploy/cache-local-path-provisioner
	echo; \
	export PATH="$(BIN_DIR):$$PATH"; \
	$(BATS) -T ./e2e/operator

clean:
	rm -rf build
	docker run --rm --privileged -v `pwd`/e2e/dcowfs:/data alpine:3.12 /bin/sh -c ' \
		umount /data/testmount/*; \
		umount /data/testmount/.cache/overlay; \
		umount /data/testmount/.cache/aufs; \
		rm -rf /data/testmount /data/fake-tls-cert'

# Build manager binary
manager: generate fmt vet
	go build -o build/bin/manager ./cmd/manager

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

kind-load-images: dcowfs-image docker-build
	kind load docker-image ${IMG}
	kind load docker-image ${DCOWFS_IMG}

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/build/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/build/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

kpt: $(KPT)
$(KPT):
	$(call download-bin,$(KPT),"https://github.com/GoogleContainerTools/kpt/releases/download/v0.39.0/kpt_$$(uname | tr '[:upper:]' '[:lower:]')_amd64")

$(BATS):
	@echo Downloading bats
	@{ \
	set -e ;\
	mkdir -p $(BIN_DIR) ;\
	TMP_DIR=$$(mktemp -d) ;\
	cd $$TMP_DIR ;\
	git clone -c 'advice.detachedHead=false' --branch v1.3.0 https://github.com/bats-core/bats-core.git . >/dev/null;\
	./install.sh $(BATS_DIR) ;\
	ln -s $(BATS_DIR)/bin/bats $(BATS) ;\
	}

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/build/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# download-bin downloads a binary into the location given as first argument
define download-bin
@[ -f $(1) ] || { \
set -e ;\
mkdir -p `dirname $(1)` ;\
TMP_FILE=$$(mktemp) ;\
echo "Downloading $(2)" ;\
curl -fsSLo $$TMP_FILE $(2) ;\
chmod +x $$TMP_FILE ;\
mv $$TMP_FILE $(1) ;\
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

minikube-start:
	minikube start --kubernetes-version=1.20.5 --network-plugin=cni --enable-default-cni --container-runtime=cri-o --bootstrapper=kubeadm

minikube-load-images: dcowfs-image
	minikube image load $(DCOWFS_IMG)
