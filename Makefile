# Version
GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))

# Defaults
REGISTRY        ?= ghcr.io
REPOSITORY      ?= projectcapsule/capsule-proxy
GIT_TAG_COMMIT  ?= $(shell git rev-parse --short $(VERSION))
GIT_MODIFIED_1  ?= $(shell git diff $(GIT_HEAD_COMMIT) $(GIT_TAG_COMMIT) --quiet && echo "" || echo ".dev")
GIT_MODIFIED_2  ?= $(shell git diff --quiet && echo "" || echo ".dirty")
GIT_MODIFIED    ?= $(shell echo "$(GIT_MODIFIED_1)$(GIT_MODIFIED_2)")
GIT_REPO        ?= $(shell git config --get remote.origin.url)
BUILD_DATE      ?= $(shell git log -1 --format="%at" | xargs -I{} sh -c 'if [ "$(shell uname)" = "Darwin" ]; then date -r {} +%Y-%m-%dT%H:%M:%S; else date -d @{} +%Y-%m-%dT%H:%M:%S; fi')
IMG_BASE        ?= $(REPOSITORY)
IMG             ?= $(IMG_BASE):$(VERSION)
CAPSULE_PROXY_IMG     ?= $(REGISTRY)/$(IMG_BASE)


OS := $(shell uname)
SRC_ROOT = $(shell git rev-parse --show-toplevel)
ifeq ($(OS),Darwin)
	ROOTCA=~/Library/Application\ Support/mkcert/rootCA.pem
else
	ROOTCA=~/.local/share/mkcert/rootCA.pem
endif

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

####################
# -- Docker
####################

dlv-build:
	docker build . --build-arg "GCFLAGS=all=-N -l" --tag projectcapsule/capsule-proxy:dlv --target dlv


KOCACHE         ?= /tmp/ko-cache
KO_TAGS         ?= "latest"

KO_TAGS         ?= "latest"
ifdef VERSION
KO_TAGS         := $(KO_TAGS),$(VERSION)
endif


LD_FLAGS        := "-X main.Version=$(VERSION) \
					-X main.GitCommit=$(GIT_HEAD_COMMIT) \
					-X main.GitTag=$(VERSION) \
					-X main.GitTreeState=$(GIT_MODIFIED) \
					-X main.BuildDate=$(BUILD_DATE) \
					-X main.GitRepo=$(GIT_REPO)"

# Docker Image Build
# ------------------

.PHONY: ko-build-capsule-proxy
ko-build-capsule-proxy: ko
	@echo Building Capsule Proxy $(KO_TAGS) >&2
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAPSULE_PROXY_IMG) \
		$(KO) build ./ --bare --tags=$(KO_TAGS) --local --push=false

.PHONY: ko-build-all
ko-build-all: ko-build-capsule-proxy

# Docker Image Publish
# ------------------

REGISTRY_PASSWORD   ?= dummy
REGISTRY_USERNAME   ?= dummy

.PHONY: ko-login
ko-login: ko
	@$(KO) login $(REGISTRY) --username $(REGISTRY_USERNAME) --password $(REGISTRY_PASSWORD)

.PHONY: ko-publish-capsule-proxy
ko-publish-capsule-proxy: ko-login
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAPSULE_PROXY_IMG) \
		$(KO) build ./ --bare --tags=$(KO_TAGS)

.PHONY: ko-publish-all
ko-publish-all: ko-publish-capsule-proxy


####################
# -- Helm
####################

helm-controller-version:
	$(eval VERSION := $(shell grep 'appVersion:' charts/capsule-proxy/Chart.yaml | awk '{print "v"$$2}'))
	$(eval KO_TAGS := $(shell grep 'appVersion:' charts/capsule-proxy/Chart.yaml | awk '{print "v"$$2}'))

.PHONY: helm-docs
helm-docs: HELMDOCS_VERSION := v1.11.0
helm-docs: docker
	@docker run -v "$(SRC_ROOT):/helm-docs" jnorwood/helm-docs:$(HELMDOCS_VERSION) --chart-search-root /helm-docs

.PHONY: helm-lint
helm-lint: docker
	@docker run -v "$(SRC_ROOT):/workdir" --entrypoint /bin/sh quay.io/helmpack/chart-testing:v3.3.1 -c "cd /workdir; ct lint --config .github/configs/ct.yaml --lint-conf .github/configs/lintconf.yaml --all --debug"

helm-test: helm-controller-version kind ct ko-build-all
	@kind create cluster --wait=60s --name capsule-charts
	@kind load docker-image --name capsule-charts $(CAPSULE_PROXY_IMG):$(VERSION)
	@kubectl create ns capsule-system
	@make helm-install

helm-install:
	@kubectl apply --server-side=true -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.2/cert-manager.yaml
	@make install-capsule
	@kubectl apply --server-side=true -f https://github.com/prometheus-operator/prometheus-operator/releases/download/v0.58.0/bundle.yaml
	@ct install --config $(SRC_ROOT)/.github/configs/ct.yaml --namespace=capsule-system --all --debug

helm-destroy:
	@kind delete cluster --name capsule-charts

####################
# -- Testing
####################

.PHONY: e2e
e2e: e2e-build e2e-install e2e-exec

.PHONY: e2e-exec
e2e-exec:
	@./e2e/run.bash $${CLIENT_TEST:-kubectl}-$${CAPSULE_PROXY_MODE:-https}

.PHONY: e2e-build
e2e-build:
	@echo "Building kubernetes env using Kind $${KIND_K8S_VERSION:-v1.22.0}..."
	@kind create cluster --name capsule --image kindest/node:$${KIND_K8S_VERSION:-v1.22.0} --config ./e2e/kind.yaml --wait=120s \
		&& kubectl taint nodes capsule-worker2 key1=value1:NoSchedule
	@helm repo add bitnami https://charts.bitnami.com/bitnami
	@helm repo update
	@helm upgrade --install --namespace metrics-system --create-namespace metrics-server bitnami/metrics-server \
		--set apiService.create=true --set "extraArgs[0]=--kubelet-insecure-tls=true" --version 6.2.9
	@echo "Waiting for metrics-server pod to be ready for listing metrics"
	@kubectl --namespace metrics-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=metrics-server

.PHONY: e2e-install
e2e-install: install-capsule install-capsule-proxy rbac-fix

.PHONY: e2e-load-image
e2e-load-image: ko-build-all
	@echo "Loading Docker image..."
	@kind load docker-image --name capsule --nodes capsule-worker $(CAPSULE_PROXY_IMG):$(VERSION)

.PHONY: e2e-destroy
e2e-destroy:
	kind delete cluster --name capsule

install-capsule:
	@echo "Installing capsule..."
	@helm repo add projectcapsule https://projectcapsule.github.io/charts
	@helm upgrade --install --create-namespace --namespace capsule-system capsule projectcapsule/capsule \
		--set "manager.resources=null" \
		--set "manager.options.forceTenantPrefix=true" \
		--set "options.logLevel=8"

install-capsule-proxy: mkcert e2e-load-image
	@echo "Installing Capsule-Proxy..."
ifeq ($(CAPSULE_PROXY_MODE),http)
	@echo "Running in HTTP mode"
	@echo "kubeconfig configurations..."
	@cd hack \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- alice oil capsule.clastix.io \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server http://127.0.0.1:9001
	@echo "Installing Capsule-Proxy using HELM..."
	@helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=$(VERSION)" \
		--set "options.enableSSL=false" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "kind=DaemonSet" \
		--set "daemonset.hostNetwork=true" \
		--set "serviceMonitor.enabled=false" \
		--set "options.generateCertificates=false"
else
	@echo "Running in HTTPS mode"
	@echo "capsule proxy certificates..."
	cd hack && $(MKCERT) -install && $(MKCERT) 127.0.0.1  \
		&& kubectl --namespace capsule-systemdelete secret capsule-proxy \
		&& kubectl --namespace capsule-system create secret generic capsule-proxy --from-file=tls.key=./127.0.0.1-key.pem --from-file=tls.crt=./127.0.0.1.pem --from-literal=ca=$$(cat $(ROOTCA) | base64 |tr -d '\n')
	@echo "kubeconfig configurations..."
	@cd hack \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- alice oil capsule.clastix.io \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- bob gas capsule.clastix.io \
		&& mv bob-gas.kubeconfig bob.kubeconfig \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- joe gas capsule.clastix.io,foo.clastix.io \
		&& mv joe-gas.kubeconfig foo.clastix.io.kubeconfig \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- dave soil capsule.clastix.io,bar.clastix.io \
		&& mv dave-soil.kubeconfig dave.kubeconfig \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.server https://127.0.0.1:9001
	@echo "Installing Capsule-Proxy using HELM..."
	@helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=$(VERSION)" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "kind=DaemonSet" \
		--set "daemonset.hostNetwork=true" \
		--set "serviceMonitor.enabled=false"
endif

rbac-fix:
	@echo "RBAC customization..."
	@kubectl create clusterrole capsule-selfsubjectaccessreviews --verb=create --resource=selfsubjectaccessreviews.authorization.k8s.io
	@kubectl create clusterrole capsule-apis --verb="get" --non-resource-url="/api/*" --non-resource-url="/api" --non-resource-url="/apis/*" --non-resource-url="/apis" --non-resource-url="/version"
	@kubectl create clusterrolebinding capsule:selfsubjectaccessreviews --clusterrole=capsule-selfsubjectaccessreviews --group=capsule.clastix.io
	@kubectl create clusterrolebinding capsule:apis --clusterrole=capsule-apis --group=capsule.clastix.io


.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=charts/capsule-proxy/crds

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

golint: golangci-lint ## Linting the code according to the styling guide.
	$(GOLANGCI_LINT) run -c .golangci.yml

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kubectl apply -f charts/capsule-proxy/crds

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kubectl delete -f charts/capsule-proxy/crds

####################
# -- Tools
####################

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
CONTROLLER_GEN_VERSION = v0.8.0
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION))

MKCERT = $(shell pwd)/bin/mkcert
MKCERT_VERSION = v1.4.4
mkcert: ## Download mkcert locally if necessary.
	$(call go-install-tool,$(MKCERT),filippo.io/mkcert@$(MKCERT_VERSION))

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
GOLANGCI_LINT_VERSION = v1.51.2
golangci-lint: ## Download golangci-lint locally if necessary.
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

CT         := $(shell pwd)/bin/ct
CT_VERSION := v3.7.1
ct: ## Download ct locally if necessary.
	$(call go-install-tool,$(CT),github.com/helm/chart-testing/v3/ct@$(CT_VERSION))

KIND         := $(shell pwd)/bin/kind
KIND_VERSION := v0.17.0
kind: ## Download kind locally if necessary.
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind/cmd/kind@$(KIND_VERSION))

KO = $(shell pwd)/bin/ko
KO_VERSION = v0.14.1
ko:
	$(call go-install-tool,$(KO),github.com/google/ko@$(KO_VERSION))

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
}
endef

docker:
	@hash docker 2>/dev/null || {\
		echo "You need docker" &&\
		exit 1;\
	}
