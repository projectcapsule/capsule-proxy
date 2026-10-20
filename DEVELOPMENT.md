# How to

## Development environment

Install [Devbox](https://www.jetify.com/docs/devbox/installing_devbox), then run:

```sh
devbox install
devbox shell
task hooks:install
task --list
```

Devbox locks the development tools in `devbox.json` and `devbox.lock`. Taskfile is
the command entry point for local work and GitHub Actions; outside the shell use
`devbox run -- task <name>`. Docker must be running for image and cluster tasks.
The `hooks:install` task replaces existing pre-commit hooks with prek and installs
commit-message checks. It runs only when explicitly requested.

Useful commands:

```sh
task test                # unit tests, race detector and coverage
task test:cli            # CLI regressions
task lint                # Go lint
task build               # bin/capsule-proxy, built from the repository root
task check               # local CI checks; no cluster required
task hooks:check         # run pre-commit hooks on all tracked files
```

Commit messages use `chore`, `ci`, `docs`, `feat`, `test`, `fix`, or `sec`, with an
optional scope and breaking-change marker, for example `fix(proxy): retain selectors`.

## Tests

Run `task test` for unit tests with the race detector, or `task test:cli` for CLI
argument coverage. End-to-end tests require the `e2e` build tag and a running
Kubernetes/Capsule/proxy environment. See [the regression test guide](e2e/README.md)
for setup, focused suite commands, coverage, and known CLI discrepancies.

## Chart Development

### Chart Linting

The chart is linted with [ct](https://github.com/helm/chart-testing). You can run the linter locally with this command:

```
task helm:lint
```

### Documentation

The documentation for each chart is done with [helm-docs](https://github.com/norwoodj/helm-docs). This way we can ensure that values are consistent with the chart documentation. Run this anytime you make changes to a `values.yaml` file:

```
task helm:docs
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
# export KUBECONFIG=<YOUR KUBERNETES CONFIGURATION FILE> or just type it before the command, i.e. `KUBECONFIG=<YOUR KUBERNETES CONFIGURATION FILE> go run . ...`
$ go run . --ssl-cert-path=/tmp/localhost.pem --ssl-key-path=/tmp/localhost-key.pem  --enable-ssl=true
```

4. Edit the `KUBECONFIG` file (you should make a copy and work on it) as follows:
- Find the section of your cluster
- replace the server path with `https://localhost:9001`
- replace the certificate-authority-data path with the content of your rootCA.pem file. (if you use mkcert, you'll find with `cat "$(mkcert -CAROOT)/rootCA.pem"|base64|tr -d '\n'`)

5. Now you should be able to run kubectl using the proxy!

## Debug in a remote Kubernetes cluster

In some cases, you would need to debug the in-cluster mode and [`delve`](https://github.com/go-delve/delve) plays a big role here.

Build and load images with the ko workflow (`task image:build`). For interactive
debugging, build the binary with Go's normal debug flags and run it under Delve
locally; the repository no longer maintains a dedicated debug container image.

## HTTP support

Capsule proxy supports `https` and `http`, although the latter is not recommended, we understand that it can be useful for some use cases (i.e. development, working behind a TLS-terminated reverse proxy and so on).

As the default behaviour is to work with `https`, we need to use the flag `--enable-ssl=false` if we really want to work under `http`.

After having **Capsule-Proxy** working under `http`, requests must provide *authentication* using an allowed Bearer Token. Example:

```bash
$ TOKEN=<type your TOKEN>
$ curl -H "Authorization: Bearer $TOKEN" http://localhost:9001/api/v1/namespaces
```

> **NOTE**: `kubectl` will not work against a http server.
