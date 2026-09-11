# Regression tests

The `e2e` package is guarded by `//go:build e2e`. It exercises a running proxy,
a real Kubernetes API server, Capsule admission webhooks, and both controllers.
No fake clients or locally constructed proxy handlers are used in these suites.

Ordinary `go test ./...` and `make test` run unit tests without Kubernetes.
`make test-cli` runs the CLI regression tests independently. `make test` also
enables the race detector and writes `coverage.out` without regenerating code.

## Running e2e

Use a disposable cluster for the full suite. Existing legacy cases use fixed
tenant names and broad test-label cleanup.

```sh
make e2e                         # provision, install, and run the entire suite
make e2e-exec                    # run against an already provisioned cluster
make e2e-exec E2E_ARGS='--label-filter=namespaced'
make e2e-exec E2E_ARGS='--label-filter=reflection'
```

The new `namespaced` and `reflection` suites generate unique tenant, namespace,
role, and subject names and delete only their own fixtures. Tests wait for tenant
status, owner RBAC, and reflection cache updates. Positive visibility checks run
before negative assertions so an unready proxy cannot pass by returning nothing.
Ordered suites share fixtures and continue independent cases after a failure.

The administrator kubeconfig selected by `KUBECONFIG` must permit impersonation.
The new suites authenticate to the proxy with those credentials and impersonate
distinct User, Group, and ServiceAccount identities. The ServiceAccount fixture
is real; its requests use impersonation, not TokenRequest credentials. The proxy
checks impersonation with the real API server. Group identities include
`projectcapsule.dev`, which the standard test CapsuleConfiguration recognizes.

By default, the proxy URL and server CA come from `hack/alice.kubeconfig` generated
by `make generate-kubeconfigs`. To test a locally running proxy instead:

```sh
KUBECONFIG=/path/to/test-cluster.kubeconfig \
E2E_PROXY_URL=https://127.0.0.1:19443 \
E2E_PROXY_CA_FILE=/path/to/proxy-ca.crt \
go test -tags e2e ./e2e -run TestE2e -timeout 30m -args \
  -ginkgo.label-filter='namespaced || reflection' -ginkgo.fail-fast
```

The proxy must run with reflection and caching enabled. The standard e2e install
also enables `ProxyClusterScoped`. Capsule's webhooks must be reachable; an API
server with offline admission webhooks is not a usable e2e environment.

## Coverage

| Suite | Resources and cases |
| --- | --- |
| Namespaced access | Pods, ConfigMaps, Secrets, Services, ServiceAccounts, PVCs, Deployments, StatefulSets, CronJobs, NetworkPolicies, and Roles |
| Subjects | Owners of one/multiple tenants, co-owners, group members, service accounts, non-owners, a user named like an owner group, the same service account name in a different namespace, and an owner with no namespaces |
| Listing behavior | Exact namespace/name sets, restricted owner RBAC, direct namespace lists and 403s, label/field selectors, conflicting tenant selectors, no selector, pagination, and ownership removal |
| Reflection | Pods, ConfigMaps, Secrets, Deployments, and NetworkPolicies; Role and ClusterRole references for User, Group, ServiceAccount, and mixed subjects |
| Reflection exclusions | Missing/false/case-mismatched labels, get-only/watch-only rules, wrong resource/API group, and resourceNames restrictions |
| Reflection changes | Wildcards, duplicate bindings, ownership plus reflection, subject replacement, label/rule grants and removal, binding deletion, deleted Role/ClusterRole references, and grants after cached denials |

Reflection currently expands resource-list access to the tenant associated with
the binding's namespace, including sibling namespaces. Namespace-scoped requests
still use Kubernetes RBAC. The suite explicitly records both behaviors.

## CLI coverage and existing discrepancies

`cli_test.go` uses the same flag registration and option construction as `main`.
An explicit inventory covers all 30 registered flags and their defaults and
fails when the inventory changes. Tests cover invalid input, repeated and empty
lists, flag-state isolation, runtime wiring, and the following behavior:

| Arguments | Assertions |
| --- | --- |
| `allowed-paths`, `public-paths` | Replacement/append semantics, exact path matching, overlap rejection, middleware routing |
| `trusted-proxy-cidrs`, `xfcc-header-name` | IPv4/IPv6 source restrictions, malformed CIDRs, custom certificate headers and wrong-header rejection |
| `auth-preferred-types`, `oidc-username-claim` | Authentication ordering, provider restriction, TLS/token/XFCC identities, TokenReview identity behavior |
| `ignored-user-group`, `ignored-username` | Matching/nonmatching identities and either-option bypass |
| `ignored-impersonation-group`, `impersonation-group-regexp` | Filtered impersonation groups and invalid regular expressions |
| `feature-gates` | Each gate on/off, isolated parses, AllAlpha, invalid gates, and real request review decisions for SkipImpersonationReview |
| `listening-port`, `enable-ssl`, `ssl-cert-path`, `ssl-key-path` | Server options, certificate/key paths, missing files, and TLS flags incompatible with HTTP |
| `disable-caching`, `enable-reflector`, `rolebindings-resync-period` | All cache/reflector combinations, actual reader selection, default/custom resync periods |
| `client-connection-qps`, `client-connection-burst` | Kubernetes REST configuration, fractional QPS, invalid numbers/overflow |
| `metrics-addr`, `enable-pprof`, `capsule-configuration-name` | Manager endpoints and selected CapsuleConfiguration controller |
| `enable-leader-election`, `webhook-port` | Parsed values and current inactive manager wiring |
| `zap-devel`, `zap-encoder`, `zap-log-level`, `zap-stacktrace-level`, `zap-time-encoding` | Actual emitted logs, verbosity, encoders, stack traces, timestamps and invalid values |

These tests establish the current refactoring baseline. They do **not** fix these
existing discrepancies between help text and runtime behavior:

- `enable-leader-election` is parsed, but manager leader election is hard-coded off.
- `webhook-port` has no effect because no webhook hooks are enabled.
- `ProxyAllNamespaced` is registered but not consulted; namespaced discovery runs regardless.
- `oidc-username-claim` is carried through options, but TokenReview supplies the username.
- `XForwardedClientCert` is advertised but rejected. The stale generated enum
  currently exposes that provider as `AuthType(3)`.
- The ISO8601 encoder override wins over `zap-time-encoding`; the dependency
  accepts `nanos` although its help advertises `nano`.
- TLS certificate/key help mentions default paths, but both flag defaults are empty.

Fixing one of these behaviors should deliberately update its regression tests.
