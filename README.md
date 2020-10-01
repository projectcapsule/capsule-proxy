# capsule-ns-filter

This project is an add-on for [Capsule](https://github.com/clastix/capsule), the operator providing multi-tenancy in Kubernetes.

## The problem

Tl;dr; In Capsule, _Tenant Owners_ are not able to list their Namespace resources:

```
$ kubectl get namespaces
Error from server (Forbidden): namespaces is forbidden: User "alice" cannot list resource "namespaces" in API group "" at the cluster scope
```

The reason, as the error message reported, is that the RBAC _list_ action is
available only at Cluster-Scope and it is not granted to the Tenant Owners. Howevers, in Capsule, Tenant Owners are always permitted to get their own namespaces:

```
$ kubectl auth can-i [get|list|watch|delete] ns oil-production
yes
```

The ability to list only the owned namespaces has been long discussed in the community, see [#48537](https://github.com/kubernetes/kubernetes/issues/48537), [#58262](https://github.com/kubernetes/kubernetes/issues/58262), and [#61958](https://github.com/kubernetes/kubernetes/issues/61958), and the objections described there still hold.

As reported in the issues above, there are no ACL-filtered APIs in core Kubernetes. To overcome this problem, many kubernetes distributions introduced mirrored custom resources of namespaces, called "Projects", "Workspaces", "Spaces", or similar, supported by a custom set of ACL-filtered APIs. However, this leads to radically change the user's experience of Kubernetes by introducing hard customizations that make painfull to move from one distribution to another. 

Capsule takes a different approach. As one of the key requirements, we want to keep the same user's experience on all the distributions of Kubernetes. With Capsule, users do not need to deal with custom resources to deploy their applications. They can use the basic tools they already learned and love and it just works.

## How it does work

This project is an add-on of the main [Capsule](https://github.com/clastix/capsule) operator, so make sure you have a working instance of Caspule before to attempt to use it. Use `capsule-ns-filter` if you want to list your namespaces throught the `kubectl` command line or throught a dashboard.

This project implements a simple reverse proxy intercepting the Kubernetes
`api/v1/namespaces` endpoint in order to filter only the namespaces assigned to the user. And Capsule does all the magic behind the scenes. All other endpoints are proxied transparently against the Kubernetes APIs server using the same request, so no side-effects are expected. 

The `capsule-ns-filter` can be deployed in standalone mode, e.g. running as a pod bridging any Kubernetes client to the `kube-apiserver`. Also, it can be deployed as sidecar container in a dashboard backend. 

### Does it work with kubectl?
Yes, it works by intercepting all the requests from the `kubectl` client directed to the APIs server. It works with both users who use the TLS certificate authentication and those who use OIDC. 

### Does it work with my preferred Kubernetes dashboard?
If you're using a client-only dashboard, for example [Lens](https://k8slens.dev/), the `capsule-ns-filter` can be used as in the previous case since these dashboards usually talk to the APIs server using just a `kubeconfig` file.

For web based dashboards, like the [Kubernetes Dashboard](https://github.com/kubernetes/dashboard), the `capsule-ns-filter` can be deployed as sidecar container in the backend side of the dashboard, following the well-known cloud native _Ambassador Pattern_. In such cases, the `capsule-ns-filter` intercept all the requests coming from the dashboard backend and proxies them to the Kubernetes APIs server.

## Documentation
Please, check the [docs](./docs) folder.

## Contributions
This is an open source software relased with Apache2 [license](./LICENSE). Feel free to open issues and pull requests. You're welcome.
