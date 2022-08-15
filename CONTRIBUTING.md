# How to

## Chart Development

### Chart Linting

The chart is linted with [ct](https://github.com/helm/chart-testing). You can run the linter locally with this command:

```
make helm-lint
```

### Documentation

The documentation for each chart is done with [helm-docs](https://github.com/norwoodj/helm-docs). This way we can ensure that values are consistent with the chart documentation. Run this anytime you make changes to a `values.yaml` file:

```
make helm-docs
```

## Run locally for test and debug

This guide helps new contributors to locally debug in _out or cluster_ mode the project.

1. You need to run a kind cluster and find the endpoint port of `kind-control-plane` using `docker ps`:

```bash
> docker ps
CONTAINER ID   IMAGE                  COMMAND                  CREATED          STATUS          PORTS                       NAMES
88432e392adb   kindest/node:v1.20.2   "/usr/local/bin/entr…"   32 seconds ago   Up 28 seconds   127.0.0.1:64582->6443/tcp   kind-control-plane
```

2. You need to generate TLS cert keys for localhost, you can use [mkcert](https://github.com/FiloSottile/mkcert):

```bash
> cd /tmp
> mkcert localhost
> ls
localhost-key.pem localhost.pem
```

3. Run the proxy with the following options

```bash
# Set KUBECONFIG environment variable with the Kubernetes configuration file if you are not currently using it.
# export KUBECONFIG=<YOUR KUBERNETES CONFIGURATION FILE> or just type it before the command, i.e. `KUBECONFIG=<YOUR KUBERNETES CONFIGURATION FILE> go run main.go ...`
$ go run main.go --ssl-cert-path=/tmp/localhost.pem --ssl-key-path=/tmp/localhost-key.pem  --enable-ssl=true
```

4. Edit the `KUBECONFIG` file (you should make a copy and work on it) as follows:
- Find the section of your cluster
- replace the server path with `https://localhost:9001`
- replace the certificate-authority-data path with the content of your rootCA.pem file. (if you use mkcert, you'll find with `cat "$(mkcert -CAROOT)/rootCA.pem"|base64|tr -d '\n'`)

5. Now you should be able to run kubectl using the proxy!

## Debug in a remote Kubernetes cluster

In some cases, you would need to debug the in-cluster mode and [`delve`](https://github.com/go-delve/delve) plays a big role here.

1. build the Docker image with `delve` issuing `make dlv-build`
2. with the `clastix/capsule-proxy:dlv` produced Docker image, publish it or load it to your [KinD](https://github.com/kubernetes-sigs/kind) instance (`kind load docker-image --name capsule --nodes capsule-control-plane clastix/capsule-proxy:dlv`)
3. change the Deployment image using `kubectl edit` or `kubectl set image deployment/capsule-proxy capsule-proxy=clastix/capsule-proxy:dlv`
4. wait for the image rollout (`kubectl -n capsule-system rollout status deployment/capsule-proxy`)
5. perform the port-forwarding with `kubectl -n capsule-system port-forward $(kubectl -n capsule-system get pods -l app.kubernetes.io/name=capsule-proxy --output name) 2345:2345`
6. connect using your `delve` options

> _Nota Bene_: the application could be killed by the Liveness Probe since delve will wait for the debugger connection before starting it.
> Feel free to edit and remove the probes to avoid this kind of issue.

## HTTP support

Capsule proxy supports `https` and `http`, although the latter is not recommended, we understand that it can be useful for some use cases (i.e. development, working behind a TLS-terminated reverse proxy and so on).

As the default behaviour is to work with `https`, we need to use the flag `--enable-ssl=false` if we really want to work under `http`.

After having **Capsule-Proxy** working under `http`, requests must provide *authentication* using an allowed Bearer Token. Example:

```bash
$ TOKEN=<type your TOKEN>
$ curl -H "Authorization: Bearer $TOKEN" http://localhost:9001/api/v1/namespaces
```

> **NOTE**: `kubectl` will not work against a http server.

