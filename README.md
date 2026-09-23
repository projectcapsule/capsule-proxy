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

## Documentation

You can find more detailed documentation [here](https://capsule.clastix.io/docs/general/proxy).

## Maintainers

Please, refer to the maintainers file available [here](.github/maintainers.yaml).

## Contributions

This is an open-source software released with Apache2 [license](./LICENSE). Feel free to open issues and pull requests. You're welcome!

Contributing is available in the related [guide](./CONTRIBUTING.md).
