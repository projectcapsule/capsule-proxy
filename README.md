# Capsule Proxy

This project is an add-on for [Capsule](https://github.com/clastix/capsule), the operator providing multi-tenancy in Kubernetes.

## The problem

Kubernetes RBAC cannot list only the owned cluster-scoped resources since there are no ACL-filtered APIs. For example:

```
$ kubectl get namespaces
Error from server (Forbidden): namespaces is forbidden:
User "alice" cannot list resource "namespaces" in API group "" at the cluster scope
```

However, the user can have permissions on some namespaces

```
$ kubectl auth can-i [get|list|watch|delete] ns oil-production
yes
```

The reason, as the error message reported, is that the RBAC _list_ action is available only at Cluster-Scope and it is not granted to users without appropriate permissions.

To overcome this problem, many Kubernetes distributions introduced mirrored custom resources supported by a custom set of ACL-filtered APIs. However, this leads to radically change the user's experience of Kubernetes by introducing hard customizations that make it painful to move from one distribution to another.

With **Capsule**, we took a different approach. As one of the key goals, we want to keep the same user's experience on all the distributions of Kubernetes. We want people to use the standard tools they already know and love and it should just work.

## How it works

This project is an add-on of the main [Capsule](https://github.com/clastix/capsule) operator, so make sure you have a working instance of Caspule before attempting to install it.
Use the `capsule-proxy` only if you want Tenant Owners to list their own Cluster-Scope resources.

The `capsule-proxy` implements a simple reverse proxy that intercepts only specific requests to the APIs server and Capsule does all the magic behind the scenes.

Current implementation filters the following requests:

* `/api/scheduling.k8s.io/{v1}/priorityclasses{/name}`
* `/api/v1/namespaces`
* `/api/v1/nodes{/name}`
* `/api/v1/pods?fieldSelector=spec.nodeName%3D{name}`
* `/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases/{name}`
* `/apis/metrics.k8s.io/{v1beta1}/nodes{/name}`
* `/apis/networking.k8s.io/{v1,v1beta1}/ingressclasses{/name}`
* `/apis/storage.k8s.io/v1/storageclasses{/name}`

All other requests are proxied transparently to the APIs server, so no side effects are expected. We're planning to add new APIs in the future, so PRs are welcome!

## Installation

The `capsule-proxy` can be deployed in standalone mode, e.g. running as a pod bridging any Kubernetes client to the APIs server.
Optionally, it can be deployed as a sidecar container in the backend of a dashboard.
Running outside a Kubernetes cluster is also viable, although a valid `KUBECONFIG` file must be provided, using the environment variable `KUBECONFIG` or the default file in `$HOME/.kube/config`.

An Helm Chart is available [here](./charts/capsule-proxy/README.md).

## Does it work with kubectl?

Yes, it works by intercepting all the requests from the `kubectl` client directed to the APIs server. It works with both users who use the TLS certificate authentication and those who use OIDC.

## How RBAC is put in place?

Each Tenant owner can have their capabilities managed pretty similar to a standard RBAC.

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: my-tenant
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: IngressClasses
      operations:
      - List
```

The proxy setting `kind` is an __enum__ accepting the supported resources:

- `Nodes`
- `StorageClasses`
- `IngressClasses`
- `PriorityClasses`

Each Resource kind can be granted with several verbs, such as:

- `List`
- `Update`
- `Delete`

### Namespaces

As tenant owner `alice`, you can use `kubectl` to create some namespaces:
```
$ kubectl --context alice-oidc@mycluster create namespace oil-production
$ kubectl --context alice-oidc@mycluster create namespace oil-development
$ kubectl --context alice-oidc@mycluster create namespace gas-marketing
```

and list only those namespaces:

```
$ kubectl --context alice-oidc@mycluster get namespaces
NAME                STATUS   AGE
gas-marketing       Active   2m
oil-development     Active   2m
oil-production      Active   2m
```

### Nodes

When a Tenant defines a `.spec.nodeSelector`, the nodes matching that labels can be easily retrieved.
The annotation `capsule.clastix.io/enable-node-listing` allows the ability for the owners to retrieve the node list (useful in shared HW scenarios).

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: Nodes
      operations:
        - List
  nodeSelector:
    kubernetes.io/hostname: capsule-gold-qwerty
```

```bash
$ kubectl --context alice-oidc@mycluster get nodes
NAME                    STATUS   ROLES    AGE   VERSION
capsule-gold-qwerty     Ready    <none>   43h   v1.19.1
```

#### Special routes for kubectl describe

When issuing a `kubectl describe node`, some other endpoints are put in place:

* `api/v1/pods?fieldSelector=spec.nodeName%3D{name}`
* `/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases/{name}`

These are mandatory in order to retrieve the list of the running Pods on the required node, and providing info about the lease status of it.

### Storage Classes

A Tenant may be limited to use a set of allowed Storage Class resources, as follows.

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: StorageClasses
      operations:
      - List
  storageClasses:
    allowed:
      - custom
    allowedRegex: "\\w+fs"
```

In the Kubernetes cluster we could have more Storage Class resources, some of them forbidden and non-usable by the Tenant owner.

```bash
$ kubectl --context admin@mycluster get storageclasses
NAME                 PROVISIONER              RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
cephfs               rook.io/cephfs           Delete          WaitForFirstConsumer   false                  21h
custom               custom.tls/provisioner   Delete          WaitForFirstConsumer   false                  43h
default(standard)    rancher.io/local-path    Delete          WaitForFirstConsumer   false                  43h
glusterfs            rook.io/glusterfs        Delete          WaitForFirstConsumer   false                  54m
zol                  zfs-on-linux/zfs         Delete          WaitForFirstConsumer   false                  54m
```

The expected output using `capsule-proxy` is the retrieval of the `custom` Storage Class as well the other ones matching the regex `\w+fs`.

```bash
$ kubectl --context alice-oidc@mycluster get storageclasses
NAME                 PROVISIONER              RECLAIMPOLICY   VOLUMEBINDINGMODE      ALLOWVOLUMEEXPANSION   AGE
cephfs               rook.io/cephfs           Delete          WaitForFirstConsumer   false                  21h
custom               custom.tls/provisioner   Delete          WaitForFirstConsumer   false                  43h
glusterfs            rook.io/glusterfs        Delete          WaitForFirstConsumer   false                  54m
```

### Ingress Classes

As for Storage Class, also Ingress Class can be enforced.

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: IngressClasses
      operations:
      - List
  ingressOptions:
    allowedClasses:
        allowed:
          - custom
        allowedRegex: "\\w+-lb"
```

In the Kubernetes cluster we could have more Ingress Class resources, some of them forbidden and non-usable by the Tenant owner.

```bash
$ kubectl --context admin@mycluster get ingressclasses
NAME              CONTROLLER                 PARAMETERS                                      AGE
custom            example.com/custom         IngressParameters.k8s.example.com/custom        24h
external-lb       example.com/external       IngressParameters.k8s.example.com/external-lb   2s
haproxy-ingress   haproxy.tech/ingress                                                       4d
internal-lb       example.com/internal       IngressParameters.k8s.example.com/external-lb   15m
nginx             nginx.plus/ingress                                                         5d
```

The expected output using `capsule-proxy` is the retrieval of the `custom` Ingress Class as well the other ones matching the regex `\w+-lb`.

```bash
$ kubectl --context alice-oidc@mycluster get ingressclasses
NAME              CONTROLLER                 PARAMETERS                                      AGE
custom            example.com/custom         IngressParameters.k8s.example.com/custom        24h
external-lb       example.com/external       IngressParameters.k8s.example.com/external-lb   2s
internal-lb       example.com/internal       IngressParameters.k8s.example.com/internal-lb   15m
```

### Priority Classes

Allowed PriorityClasses assigned to a Tenant Owner can be enforced as follows.

```yaml
apiVersion: capsule.clastix.io/v1beta1
kind: Tenant
metadata:
  name: oil
spec:
  owners:
  - kind: User
    name: alice
    proxySettings:
    - kind: IngressClasses
      operations:
      - List
  priorityClasses:
    allowed:
      - best-effort
    allowedRegex: "\\w+priority"
```

In the Kubernetes cluster we could have more PriorityClasses resources, some of them forbidden and non-usable by the Tenant owner.

```bash
$ kubectl --context admin@mycluster get priorityclasses.scheduling.k8s.io
NAME                      VALUE        GLOBAL-DEFAULT   AGE
custom                    1000         false            18s
maxpriority               1000         false            18s
minpriority               1000         false            18s
nonallowed                1000         false            8m54s
system-cluster-critical   2000000000   false            3h40m
system-node-critical      2000001000   false            3h40m
```

The expected output using `capsule-proxy` is the retrieval of the `custom` PriorityClass as well the other ones matching the regex `\w+priority`.

```bash
$ kubectl --context alice-oidc@mycluster get ingressclasses
NAME                      VALUE        GLOBAL-DEFAULT   AGE
custom                    1000         false            18s
maxpriority               1000         false            18s
minpriority               1000         false            18s
```

### Storage/Ingress class and PriorityClass required label

For Storage Class, Ingress Class and Priority Class resources, the `name` label reflecting the resource name is mandatory, otherwise filtering of resources cannot be put in place.

```yaml
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  labels:
    name: my-storage-class
  name: my-storage-class
provisioner: org.tld/my-storage-class
---
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  labels:
    name: external-lb
  name: external-lb
spec:
  controller: example.com/ingress-controller
  parameters:
    apiGroup: k8s.example.com
    kind: IngressParameters
    name: external-lb
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  labels:
    name: best-effort
  name: best-effort
value: 1000
globalDefault: false
description: "Priority class for best-effort Tenants"
```

## Does it work with my preferred Kubernetes dashboard?

If you're using a client-only dashboard, for example [Lens](https://k8slens.dev/), the `capsule-proxy` can be used as with `kubectl` since this dashboard usually talks to the APIs server using just a `kubeconfig` file.

![Lens dashboard](assets/images/lens.png)

For a web-based dashboard, like the [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), the `capsule-proxy` can be deployed as a sidecar container in the backend, following the well-known cloud-native _Ambassador Pattern_.

![Kubernetes dashboard](assets/images/kubernetes-dashboard.png)

## Documentation

You can find more detailed documentation [here](https://github.com/clastix/capsule/blob/master/docs/index.md).

## Contributions

This is an open-source software released with Apache2 [license](./LICENSE). Feel free to open issues and pull requests. You're welcome!

## How to

### Run locally for test and debug

This guide helps new contributors to locally debug in _out or cluster_ mode the project.

1. You need to run a kind cluster and find the endpoint port of `kind-control-plane` using `docker ps`:

```bash
❯ docker ps
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

### Debug in a remote Kubernetes cluster

In some cases, you would need to debug the in-cluster mode and [`delve`](https://github.com/go-delve/delve) plays a big role here.

1. build the Docker image with `delve` issuing `make dlv-build`
2. with the `quay.io/clastix/capsule-proxy:dlv` produced Docker image, publish it or load it to your [KinD](https://github.com/kubernetes-sigs/kind) instance (`kind load docker-image --name capsule --nodes capsule-control-plane quay.io/clastix/capsule-proxy:dlv`)
3. change the Deployment image using `kubectl edit` or `kubectl set image deployment/capsule-proxy capsule-proxy=quay.io/clastix/capsule-proxy:dlv`
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