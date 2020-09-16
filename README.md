# capsule-ns-filter

PoC for Namespace aggregation for Tenant owners.

## The problem

This is an add-on for [Capsule](https://github.com/clastix/capsule), a
multi-tenant Kubernetes Operator that provides multi-tenancy in Kubernetes.

Tl;dr; the _Tenant Owner_ is not able to list their Namespace resources:

```
# kubectl get ns
Error from server (Forbidden): namespaces is forbidden: User "alice" cannot list resource "namespaces" in API group "" at the cluster scope
```

The reason, as the error message reported, is that the _list_ action is
available only at Cluster-Scope.

This project is a simple reverse proxy intercepting the Kubernetes
`api/v1/namespaces` endpoint in order to filter according to the Capsule
business logic only the available Namespace resources assigned to the
requester.

All other endpoints are proxied against the original Kubernetes API endpoint
using the same request, so no side-effects are expected. 

## Installation

`capsule-ns-filter` doesn't need to have `cluster-admin` _RoleBinding_
although all read verbs (`GET`, `LIST`, `WATCH`) against the following
resources are mandatory:

- `namespaces`
- `tenants.capsule.clastix.io`

## FAQ

### Does it work with kubectl?

That's a feature we're working on.

<!-- TODO: track down feature with GH issues -->

### Does it work with OpenShift Console?

Actually, tested only with the _3.11_ release: with some hacks it can do the
job. 

<!-- TODO: document with further details -->
