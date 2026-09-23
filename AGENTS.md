# Agent instructions for Capsule Proxy

These instructions apply throughout this repository. Capsule Proxy's primary focus
is **speed: acting only as a thin intermediary to the Kubernetes API, with minimal
added latency and minimal deviations from native API behavior**. Its role is to
authenticate, enforce the necessary tenant access boundaries, and forward requests
efficiently. Kubernetes remains responsible for API semantics and resource
lifecycle; Capsule supplies tenant membership and namespace state. Preserve
tenant isolation while minimizing work in the proxy. Extend the existing design
and follow the conventions of the package you are changing.

## Required outcomes

- **Prioritize speed and low overhead.** Minimize added latency, allocations,
  buffering, and API round trips on every request without weakening isolation.
- **Preserve Kubernetes API behavior by default.** Limit request and response
  changes to what secure forwarding and tenant access require; justify and test
  every necessary deviation.
- **Keep the proxy an intermediary.** Delegate native API validation, persistence,
  and resource lifecycle to Kubernetes and tenant reconciliation to Capsule.
- **Implement access features through the existing proxy modules and settings
  APIs.** Extend the relevant request path, `ProxySetting`, or
  `GlobalProxySettings` instead of introducing a parallel permission mechanism.
- **Design around the effective access of each caller.** Identity, tenant
  membership, resource scope, operation, selectors, and reflected RBAC all matter.
- Align every feature with the current repository structure and architecture.
- Search for existing implementations before adding code. Reuse or extend existing
  types, helpers, modules, middleware, controllers, caches, and test fixtures.
- Every code or behavior change requires unit tests and end-to-end (e2e) tests.
  Add or extend coverage for the changed behavior; an unrelated passing suite is
  insufficient. For documentation-only changes, verify paths, commands, and claims
  against the repository instead of adding tests that merely assert text.
- Every e2e change must cover positive and negative cases with one or more real
  Tenant objects present. Add multiple tenants whenever isolation or shared state
  is involved.
- **Run e2e locally only for new feature tests and subsystems/components impacted by
  the change. The full e2e suite runs in GitHub Actions.** Collect observations with
  minimal reasoning during execution; perform forensics after the relevant suites
  have finished.
- New or materially changed performance-sensitive execution paths require benchmark
  coverage; extend existing benchmarks where appropriate.
- **All proxy request-path changes are performance critical**, including changes
  to authentication, authorization, middleware, modules, selectors, lookups,
  caches, response handling, or configuration used by requests.
- **Reuse existing local mutex-protected caches and indexed lookups wherever their
  consistency guarantees fit the operation.** Avoid repeated identity resolution,
  redundant API reviews, and full-list filtering when existing facilities apply.
- A change is not fully validated until its required checks pass. Report missing
  coverage, unavailable environments, and unrun checks explicitly.
- **Every change requires a self-review of scalability, performance, and security.**
  Explain the impact in each area, address findings and feedback, and repeat the
  review and relevant validation until the completion criteria below are met.

## Primary design direction: a fast, minimal Kubernetes API intermediary

Start feature design with the native Kubernetes request and response. Identify the
smallest intervention needed for tenant access, its added cost, and any observable
deviation from the upstream API. Prefer forwarding through the existing path with
minimal work. Establish the caller, resource scope, access policy, and upstream
identity; a successful response alone does not prove the access boundary is correct.

- Keep Kubernetes responsible for its API semantics. Avoid duplicating API-server
  validation, storage, resource lifecycle, or general-purpose policy processing in
  the proxy. Extend proxy settings only to express the access the intermediary needs.
- Preserve upstream request/response behavior outside the necessary access changes.
  Explain why each new rewrite, synthesized response, or interception is required
  and test both its intended effect and unaffected Kubernetes behavior.
- Evaluate the cost of every additional lookup, review, decode/encode, copy, and
  buffer. Choose the least work that preserves authorization and correctness; use
  measurements to justify added work on common paths.
- Start with `internal/modules/`, `internal/webserver/webserver.go`, and
  `internal/tenant/`. Reuse `modules.Module` and the existing registration path for
  intercepted resource operations; keep resource-specific behavior in its module.
- Extend `api/v1beta1/` for declarative access settings. A namespaced
  `ProxySetting` applies to its associated tenant; `GlobalProxySettings` grants
  selected cluster-resource access to matching subjects without requiring tenant
  ownership. Preserve that distinction and the Capsule identity gates.
- Prefer the generic cluster-resource model in `ClusterResource` and
  `internal/modules/clusterscoped/` for new cluster-resource access features.
  Preserve the `ProxyClusterScoped` feature-gate behavior and the legacy modules
  and proxy operations when maintaining supported compatibility paths.
- Reuse tenant/subject resolution, resource discovery, selector construction,
  operation matching, and RoleBinding reflection. Do not reduce authorization to
  a username match, a tenant-wide boolean, or possession of a resource label.
- Keep the advertised permissions in `internal/authorization/` consistent with
  actual proxy behavior. Changes to self-review responses must not advertise
  capabilities the request path cannot enforce.
- Preserve native Kubernetes RBAC for impersonated requests. Selector-based
  forwarding uses the proxy's credentials and therefore must establish its full
  access boundary before forwarding. Treat switching between these modes as a
  security-sensitive behavior change.
- Infrastructure and lifecycle changes should support this model. Explain any
  feature that cannot fit the existing modules or settings APIs; do not introduce
  a second authorization system or move Capsule's namespace policy reconciliation
  into this proxy as part of an unrelated feature.
- Demonstrate the resulting access in tests: matching and nonmatching subjects,
  selected and excluded resources, supported and unsupported operations, policy
  changes, and cross-tenant isolation.

## Start with the repository

Read [CONTRIBUTING.md](CONTRIBUTING.md), [DEVELOPMENT.md](DEVELOPMENT.md),
[e2e/README.md](e2e/README.md), and the relevant [Makefile](Makefile) targets. Check
[go.mod](go.mod), [.golangci.yml](.golangci.yml), and
[.github/workflows](.github/workflows) for the actual toolchain, formatting, and CI
requirements. Prefer executable targets and current code when older development
examples disagree with them; do not copy stale version numbers or nonexistent
targets. The executable is built from the repository root.

Before implementing a change:

1. Inspect the working tree and preserve unrelated work.
2. Identify the access behavior and its extension point, then trace the closest
   existing feature through identity resolution, tenant/settings lookup, module
   handling, upstream forwarding, response handling, and its unit/e2e tests.
3. Search the shared packages for reusable behavior, local caches, and field
   indexers; inspect their callers, lifetime, and consistency requirements.
4. Identify tenant and identity boundaries, API/CLI compatibility requirements,
   feature-gate behavior, and request-path impact. Consult the documented CLI
   discrepancies in `e2e/README.md` before assuming a parsed flag affects runtime.
5. Define the unit tests and positive/negative tenant e2e scenarios needed to prove
   the change. Select local e2e tests for the new feature and impacted components;
   leave full-suite execution to GitHub Actions. Identify new or materially changed
   performance-sensitive execution paths and plan benchmark coverage for them.

## Repository map and extension points

| Location | Responsibility and placement guidance |
| --- | --- |
| `main.go`, `cli.go` | Executable entry point, flag registration, option construction, schemes, indexes, manager setup, and controller/proxy wiring. Keep domain logic in the existing packages. |
| `cli_test.go` | Flag inventory, defaults, parsing, compatibility, and runtime wiring regression coverage. Update when CLI behavior changes. |
| `api/v1beta1/` | `ProxySetting`, `GlobalProxySettings`, `ClusterResource`, operation semantics, and status types. Capsule Tenant types come from the Capsule dependency. |
| `internal/webserver/` | HTTP server, reverse proxy, middleware/module registration, owner/settings resolution, discovery, and response handling. |
| `internal/webserver/middleware/` | Trusted-source checks, path handling, JWT checks, identity gates, logging, and metrics. Preserve middleware order and short-circuit behavior. |
| `internal/webserver/namespacegate/` | Authoritative namespace-existence checks for narrowly scoped forbidden-to-not-found response handling. |
| `internal/request/` | Request abstraction, authentication providers, request-local identity reuse, and impersonation review/header handling. |
| `internal/authorization/` | SelfSubjectAccessReview and SelfSubjectRulesReview response augmentation. Align advertised permissions with actual module behavior. |
| `internal/modules/` | Shared `Module` interface and resource handlers: generic `clusterscoped`/`namespaced`, `namespace`, `tenants`, and legacy resource modules. |
| `internal/modules/utils/`, `internal/modules/errors/` | Resource/selector helpers and Kubernetes-style module errors. Reuse these through the owning module. |
| `internal/tenant/` | Effective `ProxyTenant` access assembled from tenant ownership, proxy settings, and global settings. |
| `internal/controllers/` | Settings status reconciliation, CapsuleConfiguration identity updates, and RoleBinding reflection with subject indexes and a mutex-protected result cache. |
| `internal/indexer/` | Tenant owner and proxy-settings subject indexes. Register through the existing manager setup in `main.go`. |
| `internal/options/`, `internal/features/` | Listener, upstream client/transport, server options, and feature-gate identifiers. Keep flags and chart arguments consistent. |
| `internal/runtime/validation/` | Discovery-backed cluster-resource validation helpers. Verify call sites before assuming validation is enforced. |
| `internal/types/`, `internal/utils/` | Shared constants and serialization/GVK helpers. Prefer the most specific existing package over miscellaneous helpers. |
| `e2e/` | Ginkgo/Gomega tests against a real Kubernetes/Capsule/proxy environment, guarded by the `e2e` build tag. |
| `e2e-legacy/` | Historical Bats/shell tests. Use the current Go suite for new coverage and inspect legacy runner paths before using them. |
| `charts/capsule-proxy/` | Helm templates, values, schema, generated CRDs, chart documentation, and installation test values. |
| `hack/`, `.github/` | Development tooling, generation, lint configuration, and CI. Extend the existing workflows. |

Do not create a second routing framework, authentication pipeline, authorization
engine, tenant lookup mechanism, or testing framework for an individual feature.
Introduce a package or abstraction only when the existing structure cannot
reasonably own the behavior, and explain that decision in the change description.
Keep dependencies consistent with the existing layering and avoid circular imports.

## Coding and API conventions

- Match neighboring naming, file organization, constructor patterns, and interfaces.
  Keep changes focused; avoid unrelated renames, formatting churn, and refactors.
- Use the Go version and dependencies declared in `go.mod`. Prefer existing
  dependencies and the standard library before adding a dependency.
- Format Go code according to `.golangci.yml`, including standard-library,
  external, and `github.com/projectcapsule/capsule-proxy` import groups. Format test
  files too, even where lint configuration excludes them.
- Include the repository's copyright and Apache-2.0 SPDX headers in new Go files.
  Follow the current source convention; leave generated headers to tooling.
- Pass the caller's `context.Context` through API calls and work that can block.
  Use the HTTP request context for request work and the manager context for
  lifecycle work. Preserve cancellation and deadlines; avoid detached work per
  request or watch.
- Inject clients, readers, configuration, caches, loggers, and transports through
  existing constructors or manager setup. Avoid hidden global dependencies.
- Preserve errors with useful context and use existing Kubernetes/module error
  helpers. Handle expected NotFound/conflict cases deliberately; do not swallow
  unexpected errors or turn them into successful authorization decisions.
- Use existing Capsule metadata constants, subject kinds, condition helpers, and
  proxy operation helpers instead of duplicating strings or ownership logic.
- Treat shared/cache-owned objects as read-only. Copy before mutation, but avoid
  copying entire Tenant objects or large lists when a small projection suffices.
  Verify input immutability where helpers are shared.
- Use structured logging and the existing middleware metrics. Keep routine request
  logging inexpensive, avoid unbounded metric label values, and keep credentials
  and sensitive resource bodies out of logs.
- Keep public JSON fields, defaults, validation markers, nil/empty semantics, and
  operation compatibility intact unless the requested change alters that contract.
  For example, omitted cluster-resource operations allow List/Get, and legacy List
  also grants Get. Test old representations when affected.
- Keep flag registration and runtime wiring together in `cli.go`; extend
  `cli_test.go` for defaults, invalid input, repeated parsing, and actual behavior.
  Update chart arguments and documented discrepancies when intentionally fixing a
  compatibility baseline.

For controllers, follow the existing `SetupWithManager` and reconciliation
patterns. Reconciliation must remain idempotent and converge after retries. Reuse
status/condition helpers, predicates, and indexed queries; avoid unnecessary writes
or requeues. Cover deletion, conflicts, status conditions, and
`observedGeneration` when the change affects them.

## Tenant isolation is part of correctness

- Resolve identities through `internal/request/` and tenants/settings through the
  existing webserver, indexer, and `internal/tenant/` paths. Preserve User, Group,
  and ServiceAccount distinctions, including the ServiceAccount namespace.
- Scope resource selection and permissions to the intended subjects, tenants,
  namespaces, API groups, resources, and operations. Cluster-scoped resources and
  global grants still require explicit selection and authorization handling.
- Cache keys must include every input that changes the result, including identity,
  groups, verb, API group, resource, tenant, or namespace where relevant. Results
  for one caller or tenant must not leak into another caller's access.
- Preserve TokenReview and impersonation SubjectAccessReview behavior. Unverified
  JWT contents, arbitrary forwarded-certificate headers, and user-supplied labels
  must not become new proof of authorization. Retain trusted-source checks and
  test authentication-provider ordering when affected.
- Preserve distinctions between public paths, authenticated allowed paths, ignored
  identities, Capsule identities, and ordinary callers. Keep exact path matching
  and trusted-source restrictions intact when changing bypass behavior.
- Sanitize impersonation and hop-by-hop headers through the existing helpers.
  A filtered request forwarded with proxy credentials must not carry unauthorized
  caller-controlled impersonation headers or lose its mandatory selector.
- Account for revoked ownership, deleted tenants/namespaces, updated settings,
  Role/ClusterRole and RoleBinding changes, and missing referenced objects.
- Reflection currently expands resource-list access to the tenant associated with
  a binding's namespace, including sibling namespaces; direct namespace requests
  still use Kubernetes RBAC. Preserve and test that boundary unless explicitly
  changing it. A reflection grant must match its label, subject, role, and rule.
- Test both the intended effect on tenant A and the absence of unintended effects
  on tenant B. Global settings must affect exactly their intended subjects and
  selected resources, including subjects that do not own a tenant.

## Proxy requests: performance and behavior requirements

Treat the complete request path as performance critical: routing, authentication,
impersonation reviews, tenant/settings resolution, RBAC reflection, selectors,
upstream API I/O, logging, and response construction. A small shared-helper change
can affect every proxied Kubernetes request, including long-lived watches.

### Preserve the proxy pipeline

- Forward requests and responses with only the transformations needed for secure
  tenant access. Preserve upstream errors and protocol behavior; keep existing
  exceptions narrowly scoped and cover ordinary pass-through behavior in tests.
- Reuse `internal/webserver/webserver.go`, its middleware chains, and
  `modules.Module`. Preserve route ordering, discovery registration, feature gates,
  and fallback impersonation. Test the composed chain when changing short circuits.
- Preserve the module contract: an error rejects the operation; a nil selector
  delegates to impersonation; a non-nil selector selects filtered forwarding with
  proxy credentials. An empty selector and a nil selector are not interchangeable.
- Retain the distinction between GET, LIST, WATCH, mutations, subresources, and
  non-resource URLs. The HTTP method alone does not identify the Kubernetes verb.
  Keep group/version/resource parsing and authorization-review matching aligned.
- Combine caller selectors with mandatory access constraints without weakening the
  latter. Preserve field selectors, pagination, resource versions, and watch query
  parameters where supported. Cover empty and conflicting selections.
- Preserve streaming and protocol upgrades, cancellation, status codes, Kubernetes
  Status bodies, and response headers. Test JSON/protobuf negotiation where the
  changed handler decodes or re-encodes those responses.
- Keep `internal/webserver/namespacegate/` narrowly scoped: only an authoritative
  namespace NotFound can justify its response rewrite. Lookup failures or an
  existing namespace must preserve the upstream forbidden response.
- Align advertised self-review permissions, module behavior, proxy ServiceAccount
  RBAC, discovery, and chart configuration. Performance work must not weaken
  authentication, broaden grants, or bypass required reviews.

### Bound work on every request

- Reject or skip irrelevant work as early as correctness permits using existing
  routing and identity gates. Add API-call-count assertions when optimizing paths
  that should not need tenant lookups, reviews, or namespace reads.
- Reuse `request.ResolveUserAndGroups` and propagate the returned request so its
  context carries the resolved identity through adjacent middleware/modules. Do
  not repeat TokenReview or impersonation reviews within one request needlessly.
- Keep the distinction between the manager's cached client, the configurable
  resource reader, and `GetAPIReader()`. Preserve authoritative reads where needed.
  `--disable-caching` changes the resource reader and disables reflection; it does
  not make synthetic manager indexes available on the API server.
- Avoid cluster-wide scans, per-item API queries, repeated serialization,
  repeated discovery, and regex/selector compilation on the steady-state path.
  Prefer work proportional to the applicable tenants, settings, and bindings over
  total cluster size. Examine per-tenant and per-group review counts explicitly.
- Plan cache invalidation, pruning, update/delete handling, and concurrent access
  alongside cache reuse. Test cold, warm, invalidated, and missing-entry behavior.
- Reuse clients and transports. Do not introduce unbounded goroutines, retries,
  request queues, or response buffering. Bound concurrency and lock scope; avoid
  buffering entire watches or introducing blocking work per watch event.
- Measure added allocations, copies, lock contention, and API round trips. Avoid
  high-volume logs or expensive formatting on common allow/skip paths.

### Reuse local mutex-protected caches

- Reuse `RoleBindingReflector` in `internal/controllers/role_bindings.go` for
  reflected access results. Its `sync.RWMutex`, generation checks, and informer
  invalidation are the existing shared-cache pattern. Do not create a new cache
  per request or duplicate the reflector with a handler-local permission map.
- Match cache lifetime to the data. Resolved request identity belongs in the
  request context; reflected results depend on current RBAC and namespace state.
  Do not promote authentication results, lookup errors, or request snapshots to
  process-wide caches without an explicit freshness and invalidation design.
- Protect map access and publication, keep the hit path short, and never hold a
  shared cache lock across API I/O. Preserve generation-checked publication so a
  computation started before invalidation cannot repopulate stale permissions.
  Measure concurrent misses before adding another synchronization mechanism.
- A mutex protects access, not freshness or the contents of a returned pointer.
  Keep published values immutable or copy mutable portions on publication/return;
  a shallow slice copy does not isolate nested maps or pointers. Preserve the
  identity/group/resource distinctions in result keys.
- Reuse invalidation for RoleBindings, Roles, ClusterRoles, and Namespaces. Extend
  those dependencies when new inputs affect results. Define update/delete and
  grant/revocation behavior, keep memory growth bounded, and account for cached
  empty results. Process-local caches are independent across proxy replicas.
- Add tests for reuse, concurrent misses, invalidation during computation,
  mutation isolation, and distinct subjects/tenants. Benchmark cold misses, warm
  hits, and concurrent access for new or materially changed cache paths; report
  allocations and contention along with saved work.

### Use indexers for proxy lookups

- Prefer an existing keyed `Get` when the object name is known. For reverse or
  relational lookups, use registered field indexes with `client.MatchingFields`
  on the appropriate cached reader. Avoid listing all tenants/settings/bindings
  and filtering them in Go when an index can select candidates.
- Reuse `TenantOwnerKindField`, `SubjectKindField`, and `GlobalKindField` from
  `internal/indexer/`. Tenant owner lookup indexes `Tenant.status.owners`.
  `main.go` also registers Capsule's namespace-reference index; the reflector
  registers its own subject indexes. Retain the correct subject-kind key format.
- If a necessary index is missing, extend the owning domain and register it through
  the existing manager/reflector setup. Keep extraction deterministic, cheap, and
  free of API calls. Account for multi-valued relationships and update/delete
  behavior; do not build a second hand-maintained reverse map.
- Custom indexes exist in the controller-runtime cache. Do not send synthetic
  fields to `GetAPIReader()` or another direct client. Check which reader a module
  actually receives. Preserve `managerReader` for indexed owner/settings lookup,
  including when ordinary resource caching is disabled. Do not silently replace
  index registration failures with full-cluster scans.
- Indexed reads are eventually consistent. Preserve authoritative reads where
  required; re-reading returned candidates cannot repair a stale index that omitted
  a match. An empty indexed result must not become an unsafe allow decision.
- Test extraction and lookup behavior, including no/multiple matches, changed
  relationships, deletion, and subject/tenant/namespace separation. Register the
  same indexes with fake clients using `WithIndex`, and exercise real cache updates
  in e2e tests. Benchmark increasing unrelated tenant/resource counts for materially
  changed lookups and verify that full-list work and redundant reads are avoided.

### Require performance evidence

Assess performance impact for every proxy request-path change. New or materially
changed execution paths require benchmarks, including applicable allow, deny, and
pass-through cases. Compare before/after measurements for changed paths; report
absolute measurements and scaling for new paths. Exercise single-tenant and
multiple-tenant workloads, representative group/settings/binding counts, and
cache/concurrency states. Include counting-reader/client tests when lookup or
TokenReview/SubjectAccessReview behavior changes.

Report timing, allocations, API-call changes, and scaling behavior. Fix regressions
or document their measured cost and required correctness tradeoff for review. If
API round trips, contention, streaming, or forwarding behavior changes, also
validate with a real-cluster workload. Fake-client benchmark timings alone cannot
establish production proxy latency.

Measure the proxy's added overhead separately from upstream API-server work where
possible. For real-cluster comparisons, record direct and proxied request latency
and throughput under comparable identities, permissions, and workloads, and explain
any differences that prevent an equivalent comparison.

## Unit tests: required for every change

For code and behavior changes:

- Add or extend neighboring `*_test.go` files using Go's `testing` package and the
  assertions already used in that package. Prefer table-driven cases for comparable
  inputs and outcomes; use descriptive case names.
- Assert observable behavior rather than duplicating implementation details. Bug
  fixes need a regression case that fails before the fix.
- Cover positive, negative, boundary, malformed/nil/empty, and dependency-error
  cases relevant to the change. Assert meaningful errors, selectors, forwarded
  identity/headers, response status/body, and preservation of unrelated fields.
- Include tenant/namespace/identity scope where the code depends on it. Shared
  tenant-independent utilities still need e2e coverage through their consuming
  feature's tenant scenarios.
- Reuse controller-runtime fake clients, discovery fakes, `httptest`, and existing
  test doubles. Register the correct schemes, indexes, and status subresources.
  Inject errors where needed; fake-client success does not prove Kubernetes
  authentication, admission, or RBAC behavior.
- For middleware/forwarding changes, test the composed handler and observable
  upstream request as well as the helper. Assert call counts where duplicate
  reviews or forwarding would change correctness or performance.
- Use isolated fixtures and cleanup. Run parallel tests only when they do not
  share mutable state. Exercise races for new caches or concurrent code.

For documentation-only changes, perform the repository consistency checks in
Required outcomes and complete the same self-review process.

## E2E tests: positive and negative tenant scenarios

Use the existing Ginkgo v2/Gomega suite in `e2e/` with the `e2e` build tag. It uses
`envtest.Environment{UseExistingCluster: true}`: the cluster must have the changed
proxy, its CRDs/RBAC, and a working Capsule installation with reachable admission
webhooks. Merely compiling the suite, using a fake client, or running against an
old proxy image is not an e2e pass. Follow `e2e/README.md` for credentials and
endpoint configuration; enable reflection/caching and feature gates required by
the selected scenarios.

### Local execution scope and GitHub coverage

- Execute locally only new feature tests and existing suites for impacted
  components. Trace shared authentication, middleware, settings, cache, and module
  dependencies to include their affected consumers.
- Use `E2E_ARGS` with Ginkgo labels or spec filters. Existing labels include
  `namespaced`, `reflection`, and `observedGeneration`; some older specs need
  `--focus` selection. Verify the intended tests are selected; zero selected tests
  is not successful validation.
- The full e2e suite runs through `make e2e` in GitHub Actions. Do not run an
  unfiltered full suite locally as a completion step or broaden a local run merely
  for extra confidence. Report scoped local and full-suite GitHub results
  separately, including when CI is pending or unavailable.
- Preserve `Ordered`/`ContinueOnFailure` behavior where fixtures are shared. The
  default runner does not enable Ginkgo parallel processes; older cases use fixed
  names and broad cleanup. Do not add `-p` without isolating the selected fixtures.
  This repository has no separate `config` or OpenShift runner targets.

### Observe during execution; perform forensics afterward

1. Before running, choose the relevant suites and prepare observation capture.
   Record commands/filters, proxy build or image, Capsule version, cluster context,
   Ginkgo seed, feature gates, and artifact locations so the run is reproducible.
2. While e2e tests execute, **keep reasoning to a minimum and collect observations**.
   Capture runner output, pass/fail/skip counts, durations, failed assertions, proxy
   and Capsule logs, and relevant Kubernetes events or resource status. Preserve
   transient evidence before cleanup. Keep updates brief and factual; avoid
   speculative diagnoses or repeated analysis of partial output.
3. Let planned relevant suites finish before performing forensics. Do not edit code,
   change the environment, or repeatedly restart tests in response to intermediate
   failures. Run remaining relevant suites when the environment is usable; record
   environment blockers and unrun suites otherwise.
4. Analyze the collected evidence together after the relevant suites finish.
   Correlate failures with identity, tenant/namespace state, upstream RBAC,
   settings, reflection invalidation, and timing. Distinguish product regressions,
   test/fixture issues, and environment failures before choosing a fix.
5. Apply fixes, then rerun failed and newly impacted suites with the updated build.
   Preserve original observations and report final scoped results separately from
   full-suite GitHub results.

### Required scenario coverage

For every behavior change, add or extend a scenario set with all applicable rows
below. **Positive and negative cases with at least one Tenant actually created are
mandatory.** A Tenant constructed only in Go does not establish tenant context.
For missing-tenant rejection or global grants to non-owners, keep another valid
tenant present so isolation remains observable.

| Scenario | Required assertions |
| --- | --- |
| Positive | An authorized actor in tenant A performs a valid operation through the proxy; verify exact resource visibility, response, and persisted state where applicable. |
| Negative | A disallowed operation is denied or filtered as specified; verify the intended reason and absence of unauthorized resources/state changes. Establish positive readiness before asserting empty results. |
| Access settings | Cover matching/nonmatching subjects, selectors, API groups/resources, operation defaults and compatibility, and relevant legacy/feature-gated paths. Check advertised self-review permissions against actual access. |
| Multiple tenants | When isolation/shared state is involved, create tenants A and B with distinct ownership/settings; prove A's permissions and cached results do not expose B's resources. |
| Cross-tenant access | Prove an actor from A cannot read or mutate B's resources through the changed path. Cover User/Group name collisions and ServiceAccount namespace distinctions when identity matching changes. |
| Reflection | For reflector changes, exercise eligible/ineligible bindings, Role and ClusterRole rules, resourceNames restrictions, subject changes, grants/revocations, and the tenant-list versus direct-namespace boundary. |
| Lifecycle | Exercise affected ownership/settings updates, deletion/recreation, role/binding/namespace changes, and cache invalidation, including grants after cached denials. |

Test implementation requirements:

- Reuse `e2e/access_fixture_test.go` for unique tenants, namespaces, subjects,
  readiness, resource listing, and cleanup. Reuse `NewNamespace`,
  `NamespaceCreation`, `ownerClient`, and kubeconfig helpers in `e2e/utils_test.go`
  where appropriate. Confirm whether each helper connects to the API server or
  proxy; authorization-under-test requests must exercise the proxy.
- Use the administrator client for fixture setup and state verification. Use
  tenant-owner or impersonated proxy clients for access assertions. The access
  fixture uses real impersonation reviews; a real ServiceAccount fixture does not
  imply its requests use TokenRequest credentials.
- Use unique names and per-test cleanup; follow the owning fixture's labels. Avoid
  broad deletion of other tests' resources. Restore shared configuration after
  tests that change it.
- Wait for tenant status, namespace assignment, owner RBAC, settings readiness,
  and reflection updates before testing decisions. Use `Eventually`/`Consistently`
  and existing timeout/poll constants; do not add sleeps to hide races.
- Negative assertions must identify the expected denial or filtering behavior.
  A timeout, transport error, unrelated RBAC rejection, or malformed fixture is not
  evidence of isolation. For list filtering, compare exact returned sets after
  proving allowed resources are visible. Re-read state after rejected mutations.
- Preserve descriptive Ginkgo labels and shared-fixture ordering. Add focused
  labels where needed without committing focused specs.
- Do not weaken assertions, increase timeouts without diagnosis, or mark required
  coverage skipped to make a failing run appear successful.

## Benchmarks: performance-sensitive execution paths

New or materially changed performance-sensitive execution paths require benchmark
coverage. These include proxy authentication/authorization, cache/index lookups,
selector construction, resource discovery, response transformation, and reflection
or reconciliation work whose cost grows with tenants, groups, namespaces, settings,
bindings, or resources. Material changes include API calls, allocations,
algorithms, locking, concurrency, or scaling behavior.

Documentation-only changes and changes that do not introduce or materially alter
performance-sensitive execution paths do not require benchmarks.

Add Go `Benchmark...` functions in adjacent `*_test.go` or `*_bench_test.go` files.
Extend existing benchmarks where available rather than introducing a separate
framework. Place new coverage with the owning module, reflector, request helper,
or webserver path; do not assume the Capsule repository's benchmarks exist here.

- Benchmark behavior at a meaningful operation boundary, including helpers through
  their callers. Trivial wrapper-only measurements are insufficient if the affected
  work happens elsewhere.
- Use deterministic fixtures, `b.ReportAllocs()`, and sub-benchmarks for relevant
  input sizes. Include one and multiple tenants, varying identity groups,
  namespaces, settings, bindings, and resources where their counts affect cost.
- Keep fixture construction outside the timed region unless setup is measured.
  Use `b.Loop()` or `b.N`/`b.ResetTimer()` as appropriate; reset mutable inputs so
  later iterations do not accidentally measure no-ops.
- Check results/errors so benchmarks cannot measure broken shortcuts. Separate
  warm-cache work from cold resolution and invalidation. Use `b.RunParallel` when
  shared-state concurrency is part of the behavior.
- Compare repeated runs on the same machine, Go version, settings, and workload.
  Record `ns/op`, `B/op`, and `allocs/op`; do not benchmark with the race detector
  for performance comparisons. Record API/review calls separately where relevant.
- Capture the baseline before changing existing behavior and rerun the same
  benchmark afterward. For new paths, report absolute measurements and scaling,
  and confirm neighboring paths have not regressed.
- Do not add arbitrary timing thresholds to unit tests. Use benchmark comparisons
  and workload evidence; keep correctness assertions deterministic.

## Commands and validation

Run commands from the repository root. Use the checked-in Makefile's tool versions
and `go.mod` instead of independently selecting newer tools.

| Purpose | Command / guidance |
| --- | --- |
| Focused unit tests | `go test -race ./path/to/changed/package/...` (replace the path). |
| Full unit suite | `make test` runs `go test -race -coverprofile coverage.out ./...` without generating code. The e2e build tag excludes cluster tests by default. |
| CLI regressions | `make test-cli`. |
| Go lint | `make golint`; use `make golint-fix` deliberately and inspect automatic fixes. Format changed files, including tests. |
| Deep-copy generation | `make generate`. |
| CRD generation | `make manifests`. Generation and manifests are separate targets; run both when API changes require both. |
| Build proxy | `mkdir -p bin && go build -o bin/capsule-proxy .`. The entry point is at the repository root. |
| Prepare local e2e cluster | `make e2e-build`, then `make e2e-install`. The first creates the KinD cluster; the second installs dependencies and builds/loads/installs the proxy. Requires Docker and the target's cluster tooling. |
| Update proxy in test cluster | `make install-capsule-proxy` rebuilds/loads/installs the proxy and regenerates test kubeconfigs; verify rollout and image before testing. |
| Scoped local e2e | `make e2e-exec E2E_ARGS='--label-filter=namespaced'`. Replace the example label with the new feature/impacted component selection. |
| Scoped reflection e2e | `make e2e-exec E2E_ARGS='--label-filter=reflection'`. Include affected namespaced consumers when appropriate. |
| Scoped unlabeled e2e | `make e2e-exec E2E_ARGS='--focus="GlobalProxySettings resource access"'`. Replace the example with the relevant spec description. |
| Custom proxy endpoint | Follow `e2e/README.md` for `KUBECONFIG`, `E2E_PROXY_URL`, and `E2E_PROXY_CA_FILE` in suites using the access fixture. Older suites may still require generated user kubeconfigs. |
| Full e2e in GitHub Actions | `make e2e` provisions, installs, and runs the full suite. Local execution uses scoped commands above. |
| Local e2e cleanup | `make e2e-destroy` after preserving observations and completing the relevant run. |
| Focused benchmarks | `go test ./path/to/changed/package -run '^$' -bench 'BenchmarkName' -benchmem -count=5` (replace path/name). |
| Chart checks | `make helm-lint`; use `make helm-test` for installation behavior. |
| Chart documentation/schema | `make helm-docs` and `make helm-schema` when chart values or documentation change. |
| Diff hygiene | `git diff --check` and review the full diff, including new files. |

Ordinary `go test ./...` is unit-only here; adding `-tags e2e` includes real-cluster
tests. Do not pass that tag to an otherwise broad unit command accidentally.
E2E, CRD installation, and chart installation commands mutate cluster state; use a
dedicated test cluster and verify the context. Inspect lifecycle targets and their
cleanup behavior: e2e uses the fixed cluster name `capsule`, chart tests use
`capsule-charts`, and e2e certificate setup can install a local mkcert CA. The
legacy Makefile runner references `e2e/run.bash` while the script is under
`e2e-legacy/`; it is not the current Go-suite entry point.

For scalability work, supplement unit benchmarks and functional e2e with a
representative disposable-cluster workload. Record tenant/group/namespace/binding
counts, resource sizes, proxy and Capsule versions, cache/reflection settings,
resource usage, latency/throughput, API-review counts, and errors. Seeding a
workload alone is not performance evidence.

## Mandatory self-review and iteration

Every change, including documentation, configuration, tests, and generated changes,
must receive a self-review before completion. Review the final diff and affected
callers against the requested behavior and this repository's requirements. Provide
a concise assessment supported by code inspection, tests, or measurements for each
area; if no impact is expected, explain why rather than omitting the area.

- **Scalability:** Assess how work and retained state grow with tenants, groups,
  namespaces, settings, bindings, resources, and concurrent requests/watches. Look
  for full-list scans, per-item API calls, unbounded caches/queues, reconciliation
  fan-out, and contention. Explain bounds as unrelated tenants/resources are added.
- **Performance:** Assess request and reconciliation latency, CPU, allocations,
  memory, API/review round trips, repeated discovery/serialization, and cache reuse.
  Follow benchmark and real-cluster evidence requirements for affected paths;
  distinguish measured results from expectations and identify regressions.
- **Security:** Assess identity/tenant/namespace isolation, authorization,
  impersonation, proxy-credential forwarding, selectors, trusted inputs, cache
  freshness/revocation, failure behavior, sensitive data exposure, and
  denial-of-service risks. Check negative cases and ensure optimizations do not
  bypass enforcement.

Use the following loop for the initial change and every subsequent revision:

1. Review the current diff and available validation evidence. Identify concrete
   findings, assumptions, missing coverage, and feedback from the user or reviewers.
2. Revise the change to address actionable findings and feedback. Add or update
   regression coverage and performance evidence where required. If feedback does
   not warrant a change, explain the decision with evidence.
3. Rerun checks affected by the revision and inspect the resulting diff. Keep local
   e2e runs scoped to affected components and complete planned suites before
   analyzing failures, as required above.
4. Repeat the self-review across all three areas until no actionable findings remain
   unresolved, feedback has been addressed, and required checks pass. Review fixes
   for new regressions; do not stop at identifying issues or rely on passing tests
   alone as evidence that the review is complete.

In the handoff or PR, summarize the scalability, performance, and security
assessments, findings addressed, validation evidence, and remaining risks or
limitations. Disclose blocked checks and unresolved findings explicitly; do not
declare the change fully validated while required evidence is missing.

## Generated files, charts, and completion

- Edit API source and Kubebuilder markers, then regenerate. Do not hand-edit
  `zz_generated.deepcopy.go` or generated CRDs under `charts/capsule-proxy/crds/`.
  For generated enums such as `internal/request/authtype_string.go`, change the
  source and use its `go:generate` directive; review CLI compatibility afterward.
- Keep API types, operation defaults/validation, status, CRDs, RBAC, discovery,
  feature gates, and tests consistent. Review generated changes for unintended
  schema/default changes. A validation helper or chart webhook template alone does
  not establish runtime enforcement; verify its actual registration and callers.
- Update chart values, templates, schema, and documentation together when affected.
  Edit `charts/capsule-proxy/README.md.gotmpl` and regenerate its README. Check the
  chart's CI values for affected deployment modes. Release version bumps belong to
  the release process unless requested.
- Keep credentials, kubeconfigs, certificates/private keys, test artifacts,
  coverage files, and benchmark output out of commits. Preserve unrelated files.
- Describe resulting proxy behavior, settings/module integration, reused extension
  points, tenant scenarios, commands/results, and required benchmark evidence in
  the handoff or PR. Identify unrun checks and concrete blockers; never claim
  success from compilation alone or omit required e2e/performance evidence.
  Separate scoped local e2e results from full-suite GitHub status and summarize
  forensic findings after relevant suites finish.
- If preparing commits or a PR, follow `commitlint.config.cjs`,
  `.github/workflows/check-commit.yml`, `.github/workflows/check-pr.yml`, and the
  repository's contribution guidance for Conventional Commit messages and titles.

Before declaring completion, verify that the change keeps the proxy a fast, thin
intermediary with minimal deviations from Kubernetes API behavior. Justify any
added request cost or API deviation. Verify that it serves tenant-aware access,
uses the existing proxy/settings extension points, follows the repository
structure, reuses available code, includes unit and tenant-aware positive/negative
e2e coverage for behavior changes, includes benchmarks for new or materially
changed performance-sensitive paths, and preserves isolation/API contracts. Assess
performance impact for every request-path change and supply the required evidence.
Explain necessary departures from the existing design. Complete the self-review
and iteration loop, including scalability, performance, and security assessments.
