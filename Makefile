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
	kind create cluster --name capsule --image kindest/node:$* --config ./e2e/kind.yaml --wait=120s \
    && kubectl taint nodes capsule-worker2 key1=value1:NoSchedule \
    && wget https://github.com/clastix/capsule/archive/refs/tags/v0.1.0-rc5.tar.gz -P hack \
    && tar -C hack/ -xvf hack/v0.1.0-rc5.tar.gz
	helm upgrade --install --create-namespace --namespace capsule-system capsule hack/capsule-0.1.0-rc5/charts/capsule \
		--set "manager.resources=null" \
		--set "manager.options.forceTenantPrefix=true" \
		--set "manager.image.tag=v0.1.0-rc5"
	# capsule-proxy certificates
	cd hack \
        && mkcert -install && mkcert 127.0.0.1 \
    	&& kubectl --namespace capsule-system create secret tls capsule-proxy --key=./127.0.0.1-key.pem --cert ./127.0.0.1.pem
	# fake kubeconfig
	cd hack \
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
        && KUBECONFIG=foo.clastix.io.kubeconfig kubectl config set clusters.kind-capsule.server https://127.0.0.1:9001
	# capsule-proxy installation
	kind load docker-image --name capsule --nodes capsule-worker quay.io/clastix/capsule-proxy:latest
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
