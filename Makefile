OS := $(shell uname)
ifeq ($(OS),Darwin)
	ROOTCA=~/Library/Application\ Support/mkcert/rootCA.pem
else
	ROOTCA=~/.local/share/mkcert/rootCA.pem
endif

dlv-build:
	docker build . --build-arg "GCFLAGS=all=-N -l" --tag quay.io/clastix/capsule-proxy:dlv --target dlv

docker-build:
	docker build . -t quay.io/clastix/capsule-proxy:latest

e2e/clean:
	kind delete cluster --name capsule

e2e/%: docker-build
	kind create cluster --name capsule --image kindest/node:$* --config ./e2e/kind.yaml --wait=120s
	helm repo add clastix https://clastix.github.io/charts
	helm upgrade --install --create-namespace --namespace capsule-system capsule clastix/capsule \
		--set "manager.resources=null" \
		--set "manager.options.forceTenantPrefix=true"
	# capsule-proxy certificates
	cd hack \
        && mkcert -install && mkcert 127.0.0.1 \
    	&& kubectl --namespace capsule-system create secret tls capsule-proxy --key=./127.0.0.1-key.pem --cert ./127.0.0.1.pem
	# fake kubeconfig
	cd hack \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- alice oil \
		&& mv alice-oil.kubeconfig alice.kubeconfig \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=alice.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001 \
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- bob gas \
		&& mv bob-gas.kubeconfig bob.kubeconfig \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.certificate-authority-data $$(cat $(ROOTCA) | base64 |tr -d '\n') \
		&& KUBECONFIG=bob.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001
	# capsule-proxy installation
	kind load docker-image --name capsule --nodes capsule-control-plane quay.io/clastix/capsule-proxy:latest
	helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=latest" \
		--set "options.enableSSL=true" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "kind=DaemonSet" \
		--set "daemonset.hostNetwork=true"
	# kubectl RBAC fix
	kubectl create clusterrole capsule-selfsubjectaccessreviews --verb=create --resource=selfsubjectaccessreviews.authorization.k8s.io
	kubectl create clusterrole capsule-apis --verb="get" --non-resource-url="/api/*" --non-resource-url="/api" --non-resource-url="/apis/*" --non-resource-url="/apis" --non-resource-url="/version"
	kubectl create clusterrolebinding capsule:selfsubjectaccessreviews --clusterrole=capsule-selfsubjectaccessreviews --group=capsule.clastix.io
	kubectl create clusterrolebinding capsule:apis --clusterrole=capsule-apis --group=capsule.clastix.io
	./e2e/run.bash
