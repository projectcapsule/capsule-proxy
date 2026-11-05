# Capsule Proxy

This project is an add-on for [Capsule](https://github.com/projectcapsule/capsule), the operator providing multi-tenancy in Kubernetes.

`capsule-proxy` allows to overcome the limitations of Kubernetes API Server on listing owned cluster-scoped resources, like Namespace, Ingress and Storage Classes, Nodes, and others covered by Capsule.

## ProxySetting access boundary

Namespace-scoped `ProxySetting` objects support delegation within their associated
tenant. Their deprecated `spec.subjects[].clusterResources` field is ignored by
the proxy and must be omitted or empty. Administrators must move any intended
cluster-resource grants to `GlobalProxySettings`, with `ProxyClusterScoped` enabled.
Access to create or modify `GlobalProxySettings` must remain administrator-controlled.

When upgrading, roll out the fixed proxy to every replica and update the
`ProxySetting` CRD. The runtime restriction also protects against grants stored
before the CRD update; updating the CRD alone does not neutralize existing grants
on older proxy replicas. Remove obsolete namespaced grants after migration.

## Global grants for non-Capsule users

By default, proxy grants require a recognized Capsule identity. To allow any
authenticated subject to use matching `GlobalProxySettings`, set
`--require-capsule-user=false` and `--feature-gates=ProxyClusterScoped=true`.
With Helm, set `options.requireCapsuleUser: false` and add the feature gate to
`options.extraArgs`.

Ignored identities keep their native forwarding behavior. For non-Capsule
callers, a global rule must match the subject, API group, resource, and operation;
other requests use caller impersonation and Kubernetes RBAC. A matching rule
whose selector selects no objects returns an empty list. Global grants do not
enable watches, mutations, or access to resources inside selected namespaces.
Global-only requests use the generic cluster-resource handlers and do not inherit
tenant ownership or reflected namespace access.

All conditions within a selector must match; separate matching rules grant the
union of their selections. GET accepts any complete matching rule. LIST can
combine identical rules or rules that differ only in one positive label value
set, preserving their other conditions. Other rule combinations return a
Kubernetes BadRequest rather than broadening access across label combinations.

Capsule is optional in this mode. If the selected CapsuleConfiguration is absent
at startup, the proxy skips Capsule configuration reconciliation, tenant indexes,
tenant delegation, and RBAC reflection. Namespace access then uses the generic
cluster-resource handlers. Install the proxy's CRDs and RBAC as usual. When the
CapsuleConfiguration exists, the existing tenant-aware behavior remains enabled.
Availability is checked at startup; restart the proxy when Capsule or the
selected configuration is installed or removed. Unexpected discovery/read errors still
fail startup.

## Documentation

You can find more detailed documentation [here](https://capsule.clastix.io/docs/general/proxy).

## Maintainers

Please, refer to the maintainers file available [here](.github/maintainers.yaml).

## Development

Use `devbox shell` and `task --list` for the locked development toolchain. See
[DEVELOPMENT.md](DEVELOPMENT.md) for setup, prek hooks, tests and chart commands.

## Contributions

This is an open-source software released with Apache2 [license](./LICENSE). Feel free to open issues and pull requests. You're welcome!

Contributing is available in the related [guide](./CONTRIBUTING.md).
