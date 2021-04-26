# Current Operator version
VERSION ?= latest
# Image registry used for all images
IMAGE_REGISTRY ?= docker.io

# Default bundle image tag
BUNDLE_IMG ?= $(IMAGE_REGISTRY)/k8storagex-manager-bundle:$(VERSION)
# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Image URL to use all building/pushing image targets
MANAGER_IMG_NAME ?= mgoltzsche/k8storagex-controller-manager
MANAGER_IMG = $(IMAGE_REGISTRY)/$(MANAGER_IMG_NAME):$(VERSION)
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
DEV_MANIFEST_DIR := $(BUILD_DIR)/dev-manifests

BATS_DIR = $(BUILD_DIR)/tools/bats
BATS = $(BIN_DIR)/bats
KPT = $(BIN_DIR)/kpt

MANAGER_BUILD_TAGS ?=

LAYERFS_IMG_NAME ?= mgoltzsche/k8storagex-layerfs
LAYERFS_IMG = $(IMAGE_REGISTRY)/$(LAYERFS_IMG_NAME):$(VERSION)
LAYERFS_BUILD_TAGS ?= exclude_graphdriver_devicemapper exclude_graphdriver_btrfs btrfs_noversion containers_image_ostree_stub containers_image_openpgp

BUILD_TAGS ?= $(LAYERFS_BUILD_TAGS) $(MANAGER_BUILD_TAGS)

STATIC_MANIFESTS = default registry
DEPLOY_TARGETS = $(addprefix deploy-,$(STATIC_MANIFESTS))
UNDEPLOY_TARGETS = $(addprefix undeploy-,$(STATIC_MANIFESTS))


all: layerfs manager

# Deploy local changes to minikube or kind cluster
deploy-kind: kind-export-kubeconfig
deploy-minikube: IMAGE_REGISTRY=localhost
deploy-minikube deploy-kind: deploy-%: images dev-manifests | $(KPT)
	$(eval MANAGER_SHA=$(shell docker images --filter=reference=$(MANAGER_IMG) --format "{{.ID}}"))
	$(eval LAYERFS_SHA=$(shell docker images --filter=reference=$(LAYERFS_IMG) --format "{{.ID}}"))
	docker tag $(MANAGER_IMG) $(MANAGER_IMG)-$(MANAGER_SHA)
	docker tag $(LAYERFS_IMG) $(LAYERFS_IMG)-$(LAYERFS_SHA)
	make $*-load-images MANAGER_IMG=$(MANAGER_IMG)-$(MANAGER_SHA) LAYERFS_IMG=$(LAYERFS_IMG)-$(LAYERFS_SHA)
	$(KPT) cfg set $(DEV_MANIFEST_DIR) manager-image $(MANAGER_IMG)-$(MANAGER_SHA)
	$(KPT) cfg set $(DEV_MANIFEST_DIR) provisioner-image $(LAYERFS_IMG)-$(LAYERFS_SHA)
	$(KPT) live apply $(DEV_MANIFEST_DIR)
	$(KPT) live status --poll-until=current --timeout=60s $(DEV_MANIFEST_DIR)

undeploy-minikube: undeploy-registry

$(DEPLOY_TARGETS): deploy-%:
	$(KPT) live apply config/static/$*
	$(KPT) live status --poll-until=current --timeout=90s config/static/$*

$(UNDEPLOY_TARGETS): undeploy-%:
	$(KPT) live status --poll-until=deleted --timeout=60s config/static/$* & \
	$(KPT) live destroy config/static/$* && wait

# Run tests
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate fmt vet manifests
	go test -tags "$(BUILD_TAGS)" ./... -coverprofile build/cover.out

# Build layerfs binary
layerfs: fmt vet
	go build -o build/bin/layerfs -tags "$(BUILD_TAGS)" ./cmd/layerfs

layerfs-image:
	docker build -t $(LAYERFS_IMG) -f Dockerfile-layerfs .

release: check-version all test images static-manifests kind-create kind-load-images deploy-registry test-e2e kind-delete docker-push

check-version:
	@! test "$(VERSION)" = latest || (echo no VERSION specified >&2; false)

test-e2e-kind: kind-create deploy-kind test-e2e kind-delete

test-e2e: test-layerfs test-manager

test-layerfs: layerfs-image $(BATS)
	export PATH="$(BIN_DIR):$$PATH"; \
	IMAGE=${LAYERFS_IMG} ./e2e/layerfs/run-tests.sh

test-manager: $(BATS)
	@printf '\nRUNNING MANAGER E2E TESTS...\n\n'
	kubectl -n k8storagex wait --for condition=Ready --timeout=60s imagepushsecret/cache-registry
	set -e; \
	export NAMESPACE=storage-testns-`date +'%Y%m%d%H%M%S'` PATH="$(BIN_DIR):$$PATH"; \
	kubectl create namespace $$NAMESPACE; \
	echo; \
	STATUS=0; \
	$(BATS) -T ./e2e/manager || STATUS=1; \
	kubectl delete namespace $$NAMESPACE; \
	exit $$STATUS

clean: clean-test-storage
	rm -rf build

clean-test-storage:
	docker run --rm --privileged -v `pwd`/e2e/layerfs:/data alpine:3.12 /bin/sh -c ' \
		umount /data/testmount/*; \
		umount /data/testmount/.cache/overlay; \
		umount /data/testmount/.cache/aufs; \
		rm -rf /data/testmount /data/fake-tls-cert'

# Build manager binary
manager: generate fmt vet
	go build -o build/bin/manager ./cmd/manager

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./cmd/manager/main.go

static-manifests: manifests set-kustomization-images | $(KPT)
	$(KPT) fn run --network config
	for MANIFEST in default registry; do \
		rm -f config/static/$$MANIFEST/Kptfile; \
		$(KPT) pkg init config/static/$$MANIFEST --name k8storagex-$$MANIFEST; \
		$(KPT) cfg create-setter config/static/$$MANIFEST manager-image $(MANAGER_IMG) --field="image"; \
		$(KPT) cfg create-setter config/static/$$MANIFEST provisioner-image $(LAYERFS_IMG) --field="image"; \
	done

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen kustomize
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

dev-manifests:
	rm -rf $(DEV_MANIFEST_DIR)
	mkdir -p `dirname $(DEV_MANIFEST_DIR)`
	cp -r config/static/registry $(DEV_MANIFEST_DIR)

set-kustomization-images:
	cd config/manager && \
	$(KUSTOMIZE) edit set image $(MANAGER_IMG_NAME)=$(MANAGER_IMG)
	cd config/provisioners/cache && \
	$(KUSTOMIZE) edit set image $(LAYERFS_IMG_NAME)=$(LAYERFS_IMG)

check-repo-unchanged:
	@[ -z "`git status --untracked-files=no --porcelain`" ] || (\
		echo 'ERROR: the build changed files tracked by git:'; \
		git status --untracked-files=no --porcelain | sed -E 's/^/  /'; \
		echo 'Please call `make static-manifests` and commit the resulting changes.'; \
		false) >&2

# Run go fmt against code
fmt:
	go fmt ./cmd/...
	go fmt ./internal/...

# Run go vet against code
vet: clean-test-storage
	go vet -tags "$(BUILD_TAGS)" ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

images: manager-image layerfs-image

# Build the manager docker image
manager-image:
	docker build -t ${MANAGER_IMG} .

# Push the docker images
docker-push:
	docker push ${MANAGER_IMG}
	docker push ${LAYERFS_IMG}

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/build/bin/controller-gen
controller-gen: clean-test-storage
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/build/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.1.2)

kpt: $(KPT)
$(KPT):
	$(call download-bin,$(KPT),"https://github.com/GoogleContainerTools/kpt/releases/download/v0.39.2/kpt_$$(uname | tr '[:upper:]' '[:lower:]')_amd64")

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
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(MANAGER_IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

minikube-start:
	minikube start --kubernetes-version=1.20.5

minikube-delete:
	minikube delete --purge

minikube-load-images:
	minikube image load $(MANAGER_IMG)
	minikube image load $(LAYERFS_IMG)

kind-create:
	kind create cluster

kind-delete:
	kind delete cluster

kind-load-images:
	kind load docker-image $(MANAGER_IMG)
	kind load docker-image $(LAYERFS_IMG)

kind-export-kubeconfig:
	kind export kubeconfig