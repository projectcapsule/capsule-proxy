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

* `api/v1/namespaces`
* `api/v1/nodes{/name}`
* `apis/storage.k8s.io/v1/storageclasses{/name}`
* `apis/networking.k8s.io/{v1,v1beta1}/ingressclasses{/name}`

All other requests are proxied transparently to the APIs server, so no side effects are expected. We're planning to add new APIs in the future, so PRs are welcome!

## Installation

The `capsule-proxy` can be deployed in standalone mode, e.g. running as a pod bridging any Kubernetes client to the APIs server.
Optionally, it can be deployed as a sidecar container in the backend of a dashboard.
Running outside a Kubernetes cluster is also viable, although a valid `KUBECONFIG` file must be provided, using the environment variable `KUBECONFIG` or the default file in `$HOME/.kube/config`.

An Helm Chart is available [here](./charts/capsule-proxy/README.md).

## Does it work with kubectl?

Yes, it works by intercepting all the requests from the `kubectl` client directed to the APIs server. It works with both users who use the TLS certificate authentication and those who use OIDC.

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
The annotation `capsule.clastix.io/enable-ingressclass-listing` allows the ability for the owners to retrieve the node list (useful in shared HW scenarios).

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/enable-ingressclass-listing: "true"
spec:
  owner:
    kind: User
    name: alice
  nodeSelector:
    clastix.capsule.io/tenant: gold
```

```bash
$ kubectl --context alice-oidc@mycluster get nodes
NAME                    STATUS   ROLES    AGE   VERSION
capsule-gold-qwerty     Ready    <none>   43h   v1.19.1
```

> The following Tenant annotations allow a sort of RBAC on the operations of the nodes:
> 
> - `capsule.clastix.io/enable-ingressclass-listing`: allows listing of nodes and node retrieval
> - `capsule.clastix.io/enable-ingressclass-update`: allows the update of the node (cordoning and uncording, node tainting)
> - `capsule.clastix.io/enable-ingressclass-deletion`: allows deletion of the node

### Storage Classes

A Tenant may be limited to use a set of allowed Storage Class resources, as follows.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/enable-storageclass-listing: "true"
spec:
  owner:
    kind: User
    name: alice
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

> The following Tenant annotations allow a sort of RBAC on the Storage Class operations:
>
> - `capsule.clastix.io/enable-storageclass-listing`: allows listing of Storage Class and Storage Classes retrieval
> - `capsule.clastix.io/enable-storageclass-update`: allows the update of the Storage Class
> - `capsule.clastix.io/enable-storageclass-deletion`: allows deletion of the Storage Class

### Ingress Classes

As for Storage Class, also Ingress Class can be enforced.

```yaml
apiVersion: capsule.clastix.io/v1alpha1
kind: Tenant
metadata:
  name: oil
  annotations:
    capsule.clastix.io/enable-storageclass-listing: "true"
spec:
  owner:
    kind: User
    name: alice
  ingressClasses:
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

The expected output using `capsule-proxy` is the retrieval of the `custom` Storage Class as well the other ones matching the regex `\w+-lb`.

```bash
$ kubectl --context admin@mycluster get ingressclasses
NAME              CONTROLLER                 PARAMETERS                                      AGE
custom            example.com/custom         IngressParameters.k8s.example.com/custom        24h
external-lb       example.com/external       IngressParameters.k8s.example.com/external-lb   2s
internal-lb       example.com/internal       IngressParameters.k8s.example.com/internal-lb   15m
```

> The following Tenant annotations allow a sort of RBAC on the Storage Class operations:
>
> - `capsule.clastix.io/enable-ingressclass-listing`: allows listing of Ingress Class and Ingress Classes retrieval
> - `capsule.clastix.io/enable-ingressclass-update`: allows the update of the Ingress Class
> - `capsule.clastix.io/enable-ingressclass-deletion`: allows deletion of the Ingress Class

### Storage and Ingress class required label

For Storage and Ingress Class resources, the `name` label reflecting the resource name is mandatory, otherwise filtering of resources cannot be put in place.

```yaml
---
apiVersion: storage.k8sio/v1
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
go run main.go --ssl-cert-path=/tmp/localhost.pem --ssl-key-path=/tmp/localhost-key.pem --enable-ssl=true --kubeconfig=<YOUR KUBERNETES CONFIGURATION FILE>
```

5. Edit the `KUBECONFIG` file (you should make a copy and work on it) as follows:
- Find the section of your cluster
- replace the server path with `https://127.0.0.1:9001`
- replace the certificate-authority-data path with the content of your rootCA.pem file. (if you use mkcert, you'll find with `cat "$(mkcert -CAROOT)/rootCA.pem"|base64|tr -d '\n'`)

6. Now you should be able to run kubectl using the proxy!

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
