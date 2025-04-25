# Version
GIT_HEAD_COMMIT ?= $(shell git rev-parse --short HEAD)
VERSION         ?= $(or $(shell git describe --abbrev=0 --tags --match "v*" 2>/dev/null),$(GIT_HEAD_COMMIT))
GO_OS 		    ?= $(shell go env GOOS)
GO_ARCH 	    ?= $(shell go env GOARCH)

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

## Kubernetes Version Support
KUBERNETES_SUPPORTED_VERSION ?= "v1.33.0"

## Tool Binaries
KUBECTL ?= kubectl
HELM ?= helm

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


KO_PLATFORM     ?= $(GOOS)/$(GO_ARCH)
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
	echo Building Capsule Proxy $(KO_TAGS) for $(KO_PLATFORM) >&2
	@LD_FLAGS=$(LD_FLAGS) KOCACHE=$(KOCACHE) KO_DOCKER_REPO=$(CAPSULE_PROXY_IMG) \
		$(KO) build ./ --bare --tags=$(KO_TAGS) --local --push=false --platform=$(KO_PLATFORM)

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
helm-docs: helm-doc
	$(HELM_DOCS) --chart-search-root ./charts

.PHONY: helm-lint
helm-lint: ct
	@$(CT) lint --config .github/configs/ct.yaml --validate-yaml=false --all --debug

helm-schema: helm-plugin-schema
	cd charts/capsule-proxy && $(HELM) schema

helm-test: helm-create helm-install helm-destroy

helm-test-ct: ct helm-load-image
	@$(CT) install --config $(SRC_ROOT)/.github/configs/ct.yaml --namespace=capsule-system --all --debug

helm-install: install-dependencies helm-test-ct

helm-create: kind
	@$(KIND) create cluster --wait=60s --name capsule-charts --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION)
	@$(KUBECTL) create ns capsule-system

helm-load-image: kind helm-controller-version ko-build-all
	@$(KIND) load docker-image --name capsule-charts $(CAPSULE_PROXY_IMG):$(VERSION)

helm-destroy: kind
	@$(KIND) delete cluster --name capsule-charts

####################
# -- Testing
####################

.PHONY: e2e
e2e: e2e-build e2e-install e2e-exec

.PHONY: e2e-legacy-exec
e2e-legacy-exec:
	@./e2e/run.bash $${CLIENT_TEST:-kubectl}-$${CAPSULE_PROXY_MODE:-https}

.PHONY: e2e-exec
e2e-exec: ginkgo
	$(GINKGO) -v -tags e2e ./e2e

.PHONY: e2e-build
e2e-build: kind
	@echo "Building kubernetes env using Kind $(KUBERNETES_SUPPORTED_VERSION)..."
	@$(KIND) create cluster --name capsule --image kindest/node:$(KUBERNETES_SUPPORTED_VERSION) --config ./e2e/kind.yaml --wait=120s \
		&& kubectl taint nodes capsule-worker2 key1=value1:NoSchedule
	@echo "Waiting for metrics-server pod to be ready for listing metrics"

.PHONY: e2e-install
e2e-install: install-dependencies install-capsule-proxy rbac-fix

.PHONY: e2e-load-image
e2e-load-image: kind ko-build-all
	@echo "Loading Docker image..."
	@$(KIND) load docker-image --name capsule $(CAPSULE_PROXY_IMG):$(VERSION)

.PHONY: e2e-destroy
e2e-destroy: kind
	$(KIND) delete cluster --name capsule

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
		--set "options.logLevel=10" \
		--set "options.pprof=true" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "kind=DaemonSet" \
		--set "daemonset.hostNetwork=true" \
		--set "serviceMonitor.enabled=false" \
		--set "options.generateCertificates=false" \
		--set "webhooks.enabled=true" \
		--set "options.extraArgs={--feature-gates=ProxyClusterScoped=true,--feature-gates=ProxyAllNamespaced=true}"
else
	@echo "Running in HTTPS mode"
	@echo "capsule proxy certificates..."
	cd hack && $(MKCERT) -install && $(MKCERT) 127.0.0.1  \
		&& kubectl --namespace capsule-system delete secret capsule-proxy || true \
		&& kubectl --namespace capsule-system create secret generic capsule-proxy --from-file=tls.key=./127.0.0.1-key.pem --from-file=tls.crt=./127.0.0.1.pem --from-literal=ca=$$(cat $(ROOTCA) | base64 |tr -d '\n')
	@echo "kubeconfig configurations..."
	@cd hack \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- alice oil projectcapsule.dev,capsule.clastix.io \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- bob gas projectcapsule.dev,capsule.clastix.io \
		&& mv bob-gas.kubeconfig bob.kubeconfig \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- joe gas projectcapsule.dev,capsule.clastix.io,foo.clastix.io \
		&& mv joe-gas.kubeconfig foo.clastix.io.kubeconfig \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/projectcapsule/capsule/main/hack/create-user.sh | bash -s -- dave soil projectcapsule.dev,capsule.clastix.io,bar.clastix.io \
		&& mv dave-soil.kubeconfig dave.kubeconfig \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.server https://127.0.0.1:9001
	@echo "Installing Capsule-Proxy using HELM..."
	@helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=$(VERSION)" \
		--set "options.logLevel=10" \
		--set "options.pprof=true" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "kind=DaemonSet" \
		--set "daemonset.hostNetwork=true" \
		--set "serviceMonitor.enabled=false" \
		--set "webhooks.enabled=true" \
		--set "options.extraArgs={--feature-gates=ProxyClusterScoped=true,--feature-gates=ProxyAllNamespaced=true}"
endif
	@kubectl rollout restart ds capsule-proxy -n capsule-system || true

install-dependencies:
	@$(KUBECTL) kustomize e2e/distro/flux/ | kubectl apply --force-conflicts --server-side=true -f -
	@$(KUBECTL) kustomize e2e/distro/objects/ | kubectl apply --force-conflicts --server-side=true -f -
	@$(MAKE) wait-for-helmreleases

wait-for-helmreleases:
	@ echo "Waiting for all HelmReleases to have observedGeneration >= 0..."
	@while [ "$$($(KUBECTL) get helmrelease -A -o jsonpath='{range .items[?(@.status.observedGeneration<0)]}{.metadata.namespace}{" "}{.metadata.name}{"\n"}{end}' | wc -l)" -ne 0 ]; do \
	  sleep 5; \
	done

rbac-fix:
	@echo "RBAC customization..."
	@kubectl create clusterrole capsule-selfsubjectaccessreviews --verb=create --resource=selfsubjectaccessreviews.authorization.k8s.io
	@kubectl create clusterrole capsule-apis --verb="get" --non-resource-url="/api/*" --non-resource-url="/api" --non-resource-url="/apis/*" --non-resource-url="/apis" --non-resource-url="/version"
	@kubectl create clusterrolebinding capsule:selfsubjectaccessreviews --clusterrole=capsule-selfsubjectaccessreviews --group=capsule.clastix.io
	@kubectl create clusterrolebinding capsule:apis --clusterrole=capsule-apis --group=capsule.clastix.io

# Run tests
.PHONY: test
test: test-clean generate manifests test-clean
	@GO111MODULE=on go test -v $(go list ./... | grep -v /e2e/) -coverprofile coverage.out

.PHONY: test-clean
test-clean: ## Clean tests cache
	@go clean -testcache

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
# -- Helpers
####################
## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

####################
# -- Helm Plugins
####################

HELM_SCHEMA_VERSION   := ""
helm-plugin-schema:
	@$(HELM) plugin install https://github.com/losisin/helm-values-schema-json.git --version $(HELM_SCHEMA_VERSION) || true

HELM_DOCS         := $(LOCALBIN)/helm-docs
HELM_DOCS_VERSION := v1.14.1
HELM_DOCS_LOOKUP  := norwoodj/helm-docs
helm-doc:
	@test -s $(HELM_DOCS) || \
	$(call go-install-tool,$(HELM_DOCS),github.com/$(HELM_DOCS_LOOKUP)/cmd/helm-docs@$(HELM_DOCS_VERSION))

####################
# -- Tools
####################
CONTROLLER_GEN         := $(LOCALBIN)/controller-gen
CONTROLLER_GEN_VERSION ?= v0.17.1
CONTROLLER_GEN_LOOKUP  := kubernetes-sigs/controller-tools
controller-gen:
	@test -s $(CONTROLLER_GEN) && $(CONTROLLER_GEN) --version | grep -q $(CONTROLLER_GEN_VERSION) || \
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_GEN_VERSION))

GINKGO         := $(LOCALBIN)/ginkgo
GINKGO_VERSION := v2.23.3
GINKGO_LOOKUP  := onsi/ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	$(call go-install-tool,$(GINKGO),github.com/$(GINKGO_LOOKUP)/v2/ginkgo@$(GINKGO_VERSION))

MKCERT         := $(LOCALBIN)/mkcert
MKCERT_VERSION := v1.4.4
MKCERT_LOOKUP  := FiloSottile/mkcert
mkcert: ## Download mkcert locally if necessary.
	$(call go-install-tool,$(MKCERT),filippo.io/mkcert@$(MKCERT_VERSION))

CT         := $(LOCALBIN)/ct
CT_VERSION := v3.12.0
CT_LOOKUP  := helm/chart-testing
ct:
	@test -s $(CT) && $(CT) version | grep -q $(CT_VERSION) || \
	$(call go-install-tool,$(CT),github.com/$(CT_LOOKUP)/v3/ct@$(CT_VERSION))

KIND         := $(LOCALBIN)/kind
KIND_VERSION := v0.27.0
KIND_LOOKUP  := kubernetes-sigs/kind
kind:
	@test -s $(KIND) && $(KIND) --version | grep -q $(KIND_VERSION) || \
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind/cmd/kind@$(KIND_VERSION))

KO           := $(LOCALBIN)/ko
KO_VERSION   := v0.17.1
KO_LOOKUP    := google/ko
ko:
	@test -s $(KO) && $(KO) -h | grep -q $(KO_VERSION) || \
	$(call go-install-tool,$(KO),github.com/$(KO_LOOKUP)@$(KO_VERSION))

GOLANGCI_LINT          := $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION  := v1.64.8
GOLANGCI_LINT_LOOKUP   := golangci/golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@test -s $(GOLANGCI_LINT) && $(GOLANGCI_LINT) -h | grep -q $(GOLANGCI_LINT_VERSION) || \
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/$(GOLANGCI_LINT_LOOKUP)/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
[ -f $(1) ] || { \
    set -e ;\
    GOBIN=$(LOCALBIN) go install $(2) ;\
}
endef
