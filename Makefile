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

dlv-build:
	docker build . --build-arg "GCFLAGS=all=-N -l" --tag clastix/capsule-proxy:dlv --target dlv


docker/build:
	@echo "Building docker image..."
	@docker build . -t clastix/capsule-proxy:latest

kind/clean:
	@echo "Deleting cluser..."
	@kind delete cluster --name capsule

kind:
 	# build environment
	@echo "Building kubernetes env using Kind $${KIND_K8S_VERSION:-v1.22.0}..."
	@kind create cluster --name capsule --image kindest/node:$${KIND_K8S_VERSION:-v1.22.0} --config ./e2e/kind.yaml --wait=120s \
		&& kubectl taint nodes capsule-worker2 key1=value1:NoSchedule
	@helm repo add bitnami https://charts.bitnami.com/bitnami
	@helm upgrade --install --namespace metrics-system --create-namespace metrics-server bitnami/metrics-server \
		--set apiService.create=true --set "extraArgs[0]=--kubelet-insecure-tls=true" --version 6.2.9
	@echo "Waiting for metrics-server pod to be ready for listing metrics"
	@kubectl --namespace metrics-system wait --for=condition=ready --timeout=320s pod -l app.kubernetes.io/instance=metrics-server

capsule:
	@echo "Installing capsule..."
	@helm repo add clastix https://clastix.github.io/charts
	@helm upgrade --install --create-namespace --namespace capsule-system capsule clastix/capsule \
		--set "manager.resources=null" \
		--set "manager.options.forceTenantPrefix=true" \
		--set "options.logLevel=8"

capsule-proxy: mkcert
	@echo "Installing Capsule-Proxy..."
	@echo "Loading Docker image..."
	@kind load docker-image --name capsule --nodes capsule-worker clastix/capsule-proxy:latest
ifeq ($(CAPSULE_PROXY_MODE),http)
	@echo "Running in HTTP mode"
	@echo "kubeconfig configurations..."
	@cd hack \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- alice oil capsule.clastix.io \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server http://127.0.0.1:9001
	@echo "Installing Capsule-Proxy using HELM..."
	@helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=latest" \
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
		&& kubectl --namespace capsule-system create secret tls capsule-proxy --key=./127.0.0.1-key.pem --cert ./127.0.0.1.pem
	@echo "kubeconfig configurations..."
	@cd hack \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- alice oil capsule.clastix.io \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- bob gas capsule.clastix.io \
		&& mv bob-gas.kubeconfig bob.kubeconfig \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- joe gas capsule.clastix.io,foo.clastix.io \
		&& mv joe-gas.kubeconfig foo.clastix.io.kubeconfig \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- dave soil capsule.clastix.io,bar.clastix.io \
		&& mv dave-soil.kubeconfig dave.kubeconfig \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& kubectl --kubeconfig=dave.kubeconfig config set clusters.kind-capsule.server https://127.0.0.1:9001
	@echo "Installing Capsule-Proxy using HELM..."
	@helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=latest" \
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


e2e: docker/build kind capsule capsule-proxy rbac-fix
	@./e2e/run.bash $${CLIENT_TEST:-kubectl}-$${CAPSULE_PROXY_MODE:-https}

# Helm
SRC_ROOT = $(shell git rev-parse --show-toplevel)

helm-docs: HELMDOCS_VERSION := v1.11.0
helm-docs: docker
	@docker run -v "$(SRC_ROOT):/helm-docs" jnorwood/helm-docs:$(HELMDOCS_VERSION) --chart-search-root /helm-docs

helm-lint: docker
	@docker run -v "$(SRC_ROOT):/workdir" --entrypoint /bin/sh quay.io/helmpack/chart-testing:v3.3.1 -c "cd /workdir; ct lint --config .github/configs/ct.yaml --lint-conf .github/configs/lintconf.yaml --all --debug"

docker:
	@hash docker 2>/dev/null || {\
		echo "You need docker" &&\
		exit 1;\
	}
	
.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=charts/capsule-proxy/crds

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: install
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	kubectl apply -f charts/capsule-proxy/crds

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kubectl delete -f charts/capsule-proxy/crds

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

MKCERT = $(shell pwd)/bin/mkcert
mkcert: ## Download mkcert locally if necessary.
	$(call go-install-tool,$(MKCERT),filippo.io/mkcert@v1.4.4)

# go-install-tool will 'go install' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Installing $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
}
endef
