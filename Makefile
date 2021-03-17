docker-build:
	docker build . -t quay.io/clastix/capsule-proxy:latest

e2e/clean:
	kind delete cluster --name capsule

e2e/%: docker-build
	kind create cluster --name capsule --image kindest/node:$* --config ./e2e/kind.yaml
	kubectl wait --for=condition=ready --timeout=320s node capsule-control-plane
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
		&& curl -s https://raw.githubusercontent.com/clastix/capsule/master/hack/create-user.sh | bash -s -- bob gas
	# capsule-proxy installation
	kind load docker-image --name capsule --nodes capsule-control-plane quay.io/clastix/capsule-proxy:latest
	helm upgrade --install capsule-proxy ./charts/capsule-proxy -n capsule-system \
		--set "image.pullPolicy=Never" \
		--set "image.tag=latest" \
		--set "options.enableSSL=true" \
		--set "service.type=NodePort" \
		--set "service.nodePort=" \
		--set "hostNetwork=true"
	./e2e/run.bash
