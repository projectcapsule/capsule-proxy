// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/textproto"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	pkgerrors "github.com/pkg/errors"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsulemeta "github.com/projectcapsule/capsule/pkg/api/meta"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	"golang.org/x/net/http/httpguts"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/authorization"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moderrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/ingressclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/metric"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespace"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespaced"
	"github.com/projectcapsule/capsule-proxy/internal/modules/node"
	"github.com/projectcapsule/capsule-proxy/internal/modules/persistentvolume"
	"github.com/projectcapsule/capsule-proxy/internal/modules/priorityclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/runtimeclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/storageclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/tenants"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	req "github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	"github.com/projectcapsule/capsule-proxy/internal/utils"
	server "github.com/projectcapsule/capsule-proxy/internal/webserver/errors"
	"github.com/projectcapsule/capsule-proxy/internal/webserver/middleware"
)

const (
	// namespaceReadinessTimeout bounds how long the proxy will delay a namespace-create
	// response while waiting for Capsule to finish provisioning the requesting tenant
	// owner's RoleBinding in the new namespace. Capsule provisions this RBAC
	// asynchronously after the Namespace object is already persisted and returned to the
	// client, so a caller that immediately acts within the namespace it was just handed
	// (as Helm's `--create-namespace` does) can otherwise be told the namespace exists
	// while still getting 403s inside it. See
	// https://github.com/projectcapsule/capsule/issues/1895.
	//
	// On timeout the response is released as-is: the namespace was already created
	// successfully, so failing the response at this point would misreport a successful
	// creation as an error. The wait is bounded end-to-end by deriving a context with this
	// deadline, so even a slow or hung API-server List cannot exceed it.
	namespaceReadinessTimeout      = 3 * time.Second
	namespaceReadinessPollInterval = 25 * time.Millisecond

	// namespaceReadinessSettleBuffer is an extra wait applied after the first matching
	// RoleBinding is seen. Capsule creates one RoleBinding per ClusterRole configured on the
	// tenant owner (e.g. a tenant owner with clusterRoles [cluster-admin,
	// capsule-namespace-deleter] gets two), and they don't necessarily land atomically —
	// waiting for only the first one can still release the response before the others (and
	// whatever specific permission the caller's very next request needs) have caught up.
	namespaceReadinessSettleBuffer = 250 * time.Millisecond
)

func NewKubeFilter(
	opts options.ListenerOpts,
	srv options.ServerOptions,
	gates featuregate.FeatureGate,
	rbReflector *controllers.RoleBindingReflector,
	clientOverride client.Reader,
	mgr ctrl.Manager,
) (Filter, error) {
	reverseProxy := httputil.NewSingleHostReverseProxy(opts.KubernetesControlPlaneURL())
	reverseProxy.FlushInterval = time.Millisecond * 100

	reverseProxyTransport, err := opts.ReverseProxyTransport()
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot create transport for reverse proxy")
	}

	reverseProxy.Transport = reverseProxyTransport

	scheme := runtime.NewScheme()
	protoEncoder := protobuf.NewSerializer(scheme, scheme)

	err = corev1.AddToScheme(scheme)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot add corev1 to scheme")
	}

	err = authorizationv1.AddToScheme(scheme)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot add authorizationv1 to scheme")
	}

	codecFactory := serializer.NewCodecFactory(scheme)
	universalDecoder := codecFactory.UniversalDeserializer()

	// rbacClient is best-effort: it backs the namespace-readiness gate
	// (waitForOwnerRoleBinding). If it can't be built we log and continue with a nil client
	// — the gate degrades to a no-op (see waitForOwnerRoleBinding's nil check) rather than
	// taking down proxy startup for a feature that is itself designed to fail open.
	rbacClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		ctrl.Log.WithName("proxy").Error(err, "cannot create kubernetes clientset for namespace RBAC readiness checks; namespace-creation readiness gating disabled")

		rbacClient = nil
	}

	kf := &kubeFilter{
		mgr:                        mgr,
		gates:                      gates,
		reader:                     clientOverride,
		writer:                     mgr.GetClient(),
		managerReader:              mgr.GetClient(),
		allowedPaths:               sets.New(opts.AllowedPaths()...),
		authTypes:                  opts.AuthTypes(),
		ignoredUserGroups:          sets.New(opts.IgnoredGroupNames()...),
		ignoredImpersonationGroups: opts.IgnoredImpersonationsGroups(),
		impersonationGroupsRegexp:  opts.ImpersonationGroupsRegexp(),
		skipImpersonationReview:    opts.SkipImpersonationReview(),
		reverseProxy:               reverseProxy,
		bearerTokenFile:            opts.BearerTokenFile(),
		bearerToken:                opts.BearerToken(),
		bearerTokenExpirationTime:  bearerExpirationTime(opts.BearerToken()),
		usernameClaimField:         opts.PreferredUsernameClaim(),
		serverOptions:              srv,
		log:                        ctrl.Log.WithName("proxy"),
		roleBindingsReflector:      rbReflector,
		rbacClient:                 rbacClient,
		protoEncoder:               protoEncoder,
		universalDecoder:           universalDecoder,
		scheme:                     scheme,
		trustedProxyCIDRs:          opts.TrustedProxyCIDRs(),
		xfcc_header:                opts.XFCCHeader(),
	}

	reverseProxy.ModifyResponse = kf.gateNamespaceCreationResponse

	return kf, nil
}

type kubeFilter struct {
	mgr                        ctrl.Manager
	allowedPaths               sets.Set[string]
	authTypes                  []req.AuthType
	ignoredUserGroups          sets.Set[string]
	ignoredImpersonationGroups []string
	impersonationGroupsRegexp  *regexp.Regexp
	skipImpersonationReview    bool
	reverseProxy               *httputil.ReverseProxy
	bearerToken                string
	bearerTokenFile            string
	bearerTokenExpirationTime  time.Time
	usernameClaimField         string
	serverOptions              options.ServerOptions
	log                        logr.Logger
	roleBindingsReflector      *controllers.RoleBindingReflector
	rbacClient                 kubernetes.Interface
	gates                      featuregate.FeatureGate
	xfcc_header                string
	trustedProxyCIDRs          []*net.IPNet

	managerReader, reader client.Reader
	writer                client.Writer
	protoEncoder          *protobuf.Serializer
	universalDecoder      runtime.Decoder
	scheme                *runtime.Scheme

	// namespacedResources holds the set of proxied namespaced resources (keyed
	// via authorization.NamespacedResourceKey) for which capsule-proxy serves
	// cross-namespace (`-A`) list/watch queries. It is used to advertise that
	// capability through the self review (auth review) APIs.
	namespacedResources sets.Set[string]
}

// NeedLeaderElection starts the proxy (webserver) independently of controller manager
// This allows distributing the load among all pods, even if they are not leaders.
func (n *kubeFilter) NeedLeaderElection() bool {
	return false
}

//nolint:funlen
func (n *kubeFilter) Start(ctx context.Context) error {
	r := mux.NewRouter()
	r.Use(n.recoveryMiddleware)

	r.Path("/_healthz").Subrouter().HandleFunc("", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	root := r.PathPrefix("").Subrouter()
	n.registerModules(ctx, root)
	root.Use(
		middleware.RequireTrustedSourceMiddleware(n.log, n.trustedProxyCIDRs),
		n.authorizationMiddleware,
		n.reverseProxyMiddleware,
		middleware.LoggerMiddleware(n.log),
		middleware.CheckPaths(n.log, n.allowedPaths, n.impersonateHandler),
		middleware.CheckJWTMiddleware(n.writer),
	)
	root.PathPrefix("/").HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		n.impersonateHandler(writer, request)
	})
	// cert-watcher integration:
	// extracting the GetCertificate function for hot reload upon certificate update.
	// This will be used only if the proxy is set to bare TLS mode.
	var getCertificateFn func(*tls.ClientHelloInfo) (*tls.Certificate, error)

	if n.serverOptions.IsListeningTLS() {
		watcher, watcherErr := certwatcher.New(n.serverOptions.TLSCertificatePath(), n.serverOptions.TLSCertificateKeyPath())
		if watcherErr != nil {
			return fmt.Errorf("cannot create certificate watcher: %w", watcherErr)
		}

		getCertificateFn = watcher.GetCertificate

		go func() {
			if startErr := watcher.Start(ctx); startErr != nil {
				panic(fmt.Errorf("cannot start certificate watcher: %w", startErr))
			}
		}()
	}

	var srv *http.Server

	go func() {
		var err error

		addr := fmt.Sprintf("0.0.0.0:%d", n.serverOptions.ListeningPort())

		if n.serverOptions.IsListeningTLS() {
			tlsConfig := &tls.Config{
				ClientCAs:      n.serverOptions.GetCertificateAuthorityPool(),
				GetCertificate: getCertificateFn,
				MinVersion:     tls.VersionTLS12,
			}

			if slices.Contains(n.authTypes, req.TLSCertificate) {
				tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
			}

			srv = &http.Server{
				Handler:           r,
				Addr:              addr,
				TLSConfig:         tlsConfig,
				ReadHeaderTimeout: 5 * time.Second,
			}

			ln, lnErr := tls.Listen("tcp", addr, tlsConfig)
			if lnErr != nil {
				panic("cannot create listener: " + lnErr.Error())
			}

			err = srv.Serve(ln)
		} else {
			srv = &http.Server{
				Handler:           r,
				Addr:              addr,
				ReadHeaderTimeout: 5 * time.Second,
			}

			err = srv.ListenAndServe()
		}

		if err != nil {
			panic(err)
		}
	}()

	<-ctx.Done()

	return srv.Shutdown(ctx)
}

func (n *kubeFilter) LivenessProbe(*http.Request) error {
	return nil
}

func (n *kubeFilter) ReadinessProbe(req *http.Request) (err error) {
	scheme := "http"
	clt := &http.Client{}

	if n.serverOptions.IsListeningTLS() {
		scheme = "https"
		clt = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					//nolint:gosec
					InsecureSkipVerify: true,
				},
			},
		}
	}

	url := fmt.Sprintf("%s://localhost:%d/_healthz", scheme, n.serverOptions.ListeningPort())

	var r *http.Request

	if r, err = http.NewRequestWithContext(req.Context(), http.MethodGet, url, nil); err != nil {
		return pkgerrors.Wrap(err, "cannot create request")
	}

	var resp *http.Response

	if resp, err = clt.Do(r); err != nil {
		return pkgerrors.Wrap(err, "cannot make local _healthz request")
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if sc := resp.StatusCode; sc != 200 {
		return fmt.Errorf("returned status code from _healthz is %d, expected 200", sc)
	}

	return nil
}

func (n *kubeFilter) BearerToken() string {
	if time.Now().After(n.bearerTokenExpirationTime) {
		n.log.V(5).Info("Token expired. Reading new token from file", "token", n.bearerToken, "token file", n.bearerTokenFile)
		token, _ := os.ReadFile(n.bearerTokenFile)
		n.bearerToken = string(token)
		n.bearerTokenExpirationTime = bearerExpirationTime(string(token))
	}

	return n.bearerToken
}

func (n *kubeFilter) reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request)

		n.log.V(5).Info("debugging request", "uri", request.RequestURI, "method", request.Method)
		n.reverseProxy.ServeHTTP(writer, request)
	})
}

func hasBearerToken(request *http.Request) bool {
	parts := strings.Fields(request.Header.Get("Authorization"))

	return len(parts) >= 2 && strings.EqualFold(parts[0], "Bearer")
}

func (n *kubeFilter) authorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !slices.Contains(authorization.Paths, request.URL.Path) {
			next.ServeHTTP(writer, request)

			return
		}

		// Self-review requests are forwarded to the API server using the
		// caller's own bearer token whenever one is available,
		// instead of having the proxy impersonate the user together with every
		// one of their groups.
		//
		// Impersonating a token that carries hundreds of groups makes the API
		// server authorize each group impersonation individually, which is
		// extremely slow.
		// A self-review answered with the caller's own
		// credentials returns the exact same result at a fraction of the cost.

		w := httptest.NewRecorder()

		if hasBearerToken(request) {
			n.reverseProxy.ServeHTTP(w, request)
		} else {
			next.ServeHTTP(w, request)
		}

		result := w.Result()

		defer func() {
			_ = result.Body.Close()
		}()

		body, err := io.ReadAll(result.Body)
		if err != nil {
			n.log.Error(err, "cannot read response body")

			return
		}

		request, username, groups, err := req.ResolveUserAndGroups(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header)
		if err != nil {
			n.handleResolveUserAndGroupsError(writer, err)

			return
		}

		//nolint:contextcheck
		proxyTenants, err := n.getTenantsForOwner(request.Context(), username, groups)
		if err != nil {
			server.HandleError(writer, err, "cannot list Tenant resources")

			return
		}

		obj, gvk, err := n.universalDecoder.Decode(body, nil, nil)
		if err != nil {
			n.log.Error(err, "cannot decode authorization object")
		}

		if err = authorization.MutateAuthorization(n.gates.Enabled(features.ProxyClusterScoped), proxyTenants, n.namespacedResources, &obj, *gvk); err != nil {
			n.log.Error(err, "cannot mutate authorization object")
		}

		var mediaType string
		if mediaType, _, err = mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil {
			n.log.Error(err, "failed to parse Content-Type header")
		}

		switch mediaType {
		case "application/json":
			if encoded, encodeErr := utils.JsonEncode(obj, n.scheme); encodeErr != nil {
				n.log.Error(encodeErr, "cannot marshal Authorization object to json")
			} else {
				body = encoded
			}
		case "application/vnd.kubernetes.protobuf":
			if encoded, encodeErr := runtime.Encode(n.protoEncoder, obj); encodeErr != nil {
				n.log.Error(encodeErr, "cannot marshal Authorization object to protobuf")
			} else {
				body = encoded
			}
		}

		for k, v := range result.Header {
			if k == "Content-Length" {
				continue
			}

			for _, sv := range v {
				writer.Header().Add(k, sv)
			}
		}

		writer.WriteHeader(result.StatusCode)

		write, err := writer.Write(body)
		if err != nil {
			n.log.Error(err, "cannot write mutated authorization object to response", "bytesWritten", write)
		}
	})
}

func (n *kubeFilter) handleRequest(request *http.Request, selector labels.Selector) {
	req.SanitizeImpersonationHeaders(request)

	selectorValue := selector.String()

	q := request.URL.Query()
	if e := q.Get("labelSelector"); len(e) > 0 {
		n.log.V(4).Info("handling current labelSelector", "selector", e)

		v := strings.Join([]string{e, selectorValue}, ",")
		q.Set("labelSelector", v)
		n.log.V(4).Info("labelSelector updated", "selector", v)
	} else {
		q.Set("labelSelector", selectorValue)
		n.log.V(4).Info("labelSelector added", "selector", selectorValue)
	}

	n.log.V(4).Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	if token := n.BearerToken(); len(token) > 0 {
		n.log.V(10).Info("Updating the token", "token", token)
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func (n *kubeFilter) impersonateHandler(writer http.ResponseWriter, request *http.Request) {
	request, username, groups, err := req.ResolveUserAndGroups(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header)
	if err != nil {
		msg := "cannot retrieve user and group"

		var t *req.ErrUnauthorized
		if errors.As(err, &t) {
			server.HandleUnauthorized(writer, err, msg)
		} else {
			server.HandleError(writer, err, msg)
		}

		return
	}

	n.log.V(4).Info("impersonating for the current request", "username", username, "groups", groups, "uri", request.URL.Path)

	if token := n.BearerToken(); len(token) > 0 {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	// Dropping malicious header connection
	// https://github.com/projectcapsule/capsule-proxy/issues/188
	n.removingHopByHopHeaders(request)

	request.Header.Add(authenticationv1.ImpersonateUserHeader, username)

	for _, group := range groups {
		request.Header.Add(authenticationv1.ImpersonateGroupHeader, group)
	}
}

// gateNamespaceCreationResponse is the single ModifyResponse entry point for both fixes
// addressing https://github.com/projectcapsule/capsule/issues/1895:
//
//   - maskForbiddenForMissingNamespace rewrites a 403 into the 404 a privileged caller would
//     see, when a tenant owner's namespace-scoped GET fails only because the target
//     namespace doesn't exist yet. Without this, clients with idiomatic "GET, if 404 then
//     create" workflows (Helm's `--create-namespace` among them) treat the 403 as a fatal
//     error and abort before ever attempting to create the namespace.
//   - waitForOwnerCreationResponse delays a successful namespace-create response until the
//     tenant owner's RoleBinding has actually landed in the new namespace, closing the
//     separate (and separately real) race between namespace creation and Capsule's async
//     RBAC provisioning for whatever request comes immediately after.
func (n *kubeFilter) gateNamespaceCreationResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusForbidden {
		return n.maskForbiddenForMissingNamespace(resp)
	}

	return n.waitForOwnerCreationResponse(resp)
}

// namespacedResourcePathPattern matches a GET-by-name request for a namespaced resource:
// /api/v1/namespaces/{ns}/{resource}/{name} or
// /apis/{group}/{version}/namespaces/{ns}/{resource}/{name}. It intentionally does not match
// collection requests (LIST, no trailing {name}) or subresources (an extra path segment
// after {name}, e.g. .../pods/{name}/log).
var namespacedResourcePathPattern = regexp.MustCompile(`^(?:/api/v1|/apis/[^/]+/[^/]+)/namespaces/([^/]+)/([^/]+)/([^/]+)$`)

// maskForbiddenForMissingNamespace rewrites a 403 into the 404 a privileged user would see
// when a tenant owner's namespace-scoped GET fails only because the target namespace
// doesn't exist yet. Kubernetes RBAC authorizes namespace-scoped requests using RoleBindings
// that live inside that namespace, so a namespace that hasn't been created yet can never
// have one — meaning any tenant owner probing a resource there always gets 403 Forbidden,
// never the 404 Not Found a cluster-wide-privileged caller gets for the same nonexistent
// namespace.
//
// The rewrite is deliberately narrow, so it never becomes a cross-tenant existence oracle:
// it applies only when the target namespace name falls within the namespace-naming space of
// a tenant the caller actually owns (see namespaceClaimableByOwner) AND the namespace is
// confirmed not to exist. A caller can therefore only ever learn "a namespace I'm entitled
// to create doesn't exist yet" — information they already have (they can list their own
// tenant's namespaces). A 403 for any namespace outside the caller's own tenants, or for one
// that genuinely exists, is passed through unchanged.
func (n *kubeFilter) maskForbiddenForMissingNamespace(resp *http.Response) error {
	if resp.Request.Method != http.MethodGet {
		return nil
	}

	username := resp.Request.Header.Get(authenticationv1.ImpersonateUserHeader)
	if username == "" {
		return nil
	}

	match := namespacedResourcePathPattern.FindStringSubmatch(resp.Request.URL.Path)
	if match == nil {
		return nil
	}

	namespaceName, resource, name := match[1], match[2], match[3]

	groups := resp.Request.Header.Values(authenticationv1.ImpersonateGroupHeader)

	// Ownership gate: only mask for a namespace the caller could legitimately create as one
	// of their own tenants' namespaces. Without this, the 403-vs-404 distinction would let
	// any tenant owner probe the existence of arbitrary namespaces cluster-wide.
	if !n.namespaceClaimableByOwner(resp.Request.Context(), username, groups, namespaceName) {
		return nil
	}

	var namespaceObj corev1.Namespace
	if err := n.reader.Get(resp.Request.Context(), client.ObjectKey{Name: namespaceName}, &namespaceObj); err == nil || !apierrors.IsNotFound(err) {
		// Namespace exists, or we couldn't tell — this is a genuine permission denial,
		// leave it as-is.
		return nil
	}

	status := apierrors.NewNotFound(schema.GroupResource{Resource: resource}, name).Status()

	body, err := json.Marshal(status)
	if err != nil {
		return nil //nolint:nilerr // best-effort; fall back to the original 403 on marshal failure
	}

	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.StatusCode = http.StatusNotFound
	resp.Status = fmt.Sprintf("%d %s", http.StatusNotFound, http.StatusText(http.StatusNotFound))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Set("Content-Type", "application/json")

	n.log.V(4).Info("masked forbidden as not-found for missing namespace", "namespace", namespaceName, "resource", resource, "name", name, "username", username)

	return nil
}

// namespaceClaimableByOwner reports whether namespaceName falls within the namespace-naming
// space of a tenant owned by the caller — i.e. it equals an owned tenant's name or is
// prefixed by "<tenant>-". This mirrors Capsule's own forceTenantPrefix boundary and is the
// only relationship the proxy can establish for a namespace that does not exist yet (it has
// no owner reference or tenant label to consult). Callers with no owned tenants, or probing
// a name outside all of their tenants' prefixes, get false.
func (n *kubeFilter) namespaceClaimableByOwner(ctx context.Context, username string, groups []string, namespaceName string) bool {
	proxyTenants, err := n.getTenantsForOwner(ctx, username, groups)
	if err != nil {
		n.log.V(4).Info("cannot resolve tenant owners while masking namespace lookup", "namespace", namespaceName, "error", err.Error())

		return false
	}

	for _, pt := range proxyTenants {
		tenantName := pt.Tenant.GetName()
		if namespaceName == tenantName || strings.HasPrefix(namespaceName, tenantName+"-") {
			return true
		}
	}

	return false
}

// waitForOwnerCreationResponse delays a successful namespace-create response until the
// requesting tenant owner's RoleBinding has actually landed in the new namespace, or until
// namespaceReadinessTimeout elapses. See the namespaceReadinessTimeout doc comment and
// https://github.com/projectcapsule/capsule/issues/1895 for why this exists.
//
// This has to match both the classic collection-POST create (`POST /api/v1/namespaces`)
// and a Server-Side Apply create (`PATCH /api/v1/namespaces/<name>`) — Helm 4 defaults
// `--server-side` to true, so `helm install --create-namespace` goes through the SSA path,
// not a plain POST. Kubernetes returns 201 for both when the object didn't previously exist,
// which is what distinguishes a genuine creation (something to gate) from an update to an
// existing namespace (RBAC already settled, nothing to wait for) on the same PATCH verb.
func (n *kubeFilter) waitForOwnerCreationResponse(resp *http.Response) error {
	if resp.StatusCode != http.StatusCreated {
		return nil
	}

	if resp.Request.Method != http.MethodPost && resp.Request.Method != http.MethodPatch {
		return nil
	}

	if !isNamespaceCollectionOrObjectPath(resp.Request.URL.Path) {
		return nil
	}

	// A server-side dry-run create (`?dryRun=All`) returns 201 but persists nothing — no
	// Namespace and no RoleBinding are actually created — so there is nothing to wait for.
	// Without this, every dry-run namespace create would block for the full readiness
	// timeout polling for a RoleBinding that will never appear.
	if resp.Request.URL.Query().Get("dryRun") != "" {
		return nil
	}

	username := resp.Request.Header.Get(authenticationv1.ImpersonateUserHeader)
	groups := resp.Request.Header.Values(authenticationv1.ImpersonateGroupHeader)

	if username == "" {
		// Not an impersonated request (e.g. a call authenticated with the proxy's own
		// bearer token) — nothing to gate.
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil //nolint:nilerr // best-effort gate; never fail a response that already succeeded upstream
	}

	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var namespace corev1.Namespace
	if err := json.Unmarshal(body, &namespace); err != nil || namespace.Name == "" {
		return nil
	}

	tenantName := namespace.Labels[capsulemeta.TenantLabel]
	if tenantName == "" {
		// Not a Capsule tenant namespace.
		return nil
	}

	if !n.isTenantOwner(resp.Request.Context(), tenantName, username, groups) {
		// Whoever created this namespace isn't a registered owner of its tenant (e.g. a
		// cluster administrator using capsule-proxy directly). Capsule only provisions an
		// owner RoleBinding for registered tenant owners, so there's nothing to wait for
		// and waiting here would only add latency to a request that already works.
		return nil
	}

	n.waitForOwnerRoleBinding(resp.Request.Context(), username, groups, namespace.Name)

	return nil
}

// isNamespaceCollectionOrObjectPath reports whether path is the namespaces collection
// endpoint ("/api/v1/namespaces") or a single namespace's object endpoint
// ("/api/v1/namespaces/<name>") — not a subresource of it (e.g. .../status, .../finalize).
func isNamespaceCollectionOrObjectPath(path string) bool {
	const collectionPath = "/api/v1/namespaces"

	if path == collectionPath {
		return true
	}

	name, ok := strings.CutPrefix(path, collectionPath+"/")

	return ok && name != "" && !strings.Contains(name, "/")
}

func (n *kubeFilter) isTenantOwner(ctx context.Context, tenantName, username string, groups []string) bool {
	proxyTenants, err := n.getTenantsForOwner(ctx, username, groups)
	if err != nil {
		n.log.V(4).Info("cannot resolve tenant owners while gating namespace creation", "tenant", tenantName, "error", err.Error())

		return false
	}

	for _, pt := range proxyTenants {
		if pt.Tenant.GetName() == tenantName {
			return true
		}
	}

	return false
}

// waitForOwnerRoleBinding polls the API server directly for a RoleBinding in namespace
// whose subjects include username or one of groups. It intentionally doesn't use the
// optional roleBindingsReflector (disabled by default via --enable-reflector=false): this
// gate needs to work regardless of that setting, and a namespace-scoped List is cheap
// enough to issue synchronously here — it's bounded to at most namespaceReadinessTimeout /
// namespaceReadinessPollInterval calls, and only for genuine tenant-owner namespace
// creations (see isTenantOwner above).
//
// The whole wait is bounded by a context deadline derived from namespaceReadinessTimeout, so
// that even a single slow or hung List call cannot hold the client's response open past the
// documented bound: once the deadline passes, the in-flight List is cancelled and the
// response is released (fail-open).
func (n *kubeFilter) waitForOwnerRoleBinding(ctx context.Context, username string, groups []string, namespace string) {
	if n.rbacClient == nil {
		return
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, namespaceReadinessTimeout)
	defer cancel()

	groupSet := sets.New(groups...)

	for {
		ready, err := n.hasMatchingRoleBinding(ctx, username, groupSet, namespace)
		if err != nil {
			// Includes context deadline exceeded — fail open, the namespace is already
			// created and the response must not be held any longer.
			n.log.V(4).Info("stopped waiting for owner RoleBinding readiness", "namespace", namespace, "username", username, "waited", time.Since(start), "reason", err.Error())

			return
		}

		if ready {
			// A tenant owner can have more than one ClusterRole configured, and Capsule
			// creates one RoleBinding per ClusterRole — they don't necessarily land in the
			// same instant. Give any siblings a short window to catch up too, rather than
			// releasing the response the moment the very first one appears. Cap the buffer
			// at the remaining deadline so it can't push total latency past the bound.
			settle := namespaceReadinessSettleBuffer
			if remaining := time.Until(start.Add(namespaceReadinessTimeout)); remaining < settle {
				settle = remaining
			}

			if settle > 0 {
				time.Sleep(settle)
			}

			n.log.V(5).Info("owner RoleBinding ready", "namespace", namespace, "username", username, "waited", time.Since(start))

			return
		}

		select {
		case <-ctx.Done():
			n.log.Info("timed out waiting for owner RoleBinding to be provisioned; releasing namespace-create response anyway", "namespace", namespace, "username", username, "timeout", namespaceReadinessTimeout)

			return
		case <-time.After(namespaceReadinessPollInterval):
		}
	}
}

func (n *kubeFilter) hasMatchingRoleBinding(ctx context.Context, username string, groups sets.Set[string], namespace string) (bool, error) {
	list, err := n.rbacClient.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, rb := range list.Items {
		for _, subject := range rb.Subjects {
			switch subject.Kind {
			case rbacv1.UserKind:
				if subject.Name == username {
					return true, nil
				}
			case rbacv1.GroupKind:
				if groups.Has(subject.Name) {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

func (n *kubeFilter) ownerFromCapsuleToProxySetting(owners capsulerbac.OwnerListSpec) []v1beta1.OwnerSpec {
	out := make([]v1beta1.OwnerSpec, 0, len(owners))

	for _, owner := range owners {
		out = append(out, v1beta1.OwnerSpec{
			Kind:            owner.Kind,
			Name:            owner.Name,
			ProxyOperations: owner.ProxyOperations,
		})
	}

	return out
}

//nolint:funlen
func (n *kubeFilter) registerModules(ctx context.Context, root *mux.Router) {
	// We are using namespaces and tenants as default routes from the legacy
	// system, as their outcome heavily relies on the tenants config/status
	modList := []modules.Module{
		namespace.List(n.roleBindingsReflector, n.reader),
		namespace.Get(n.roleBindingsReflector, n.reader),
		tenants.List(n.reader),
		tenants.Get(n.reader),
	}

	// Discovery client
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(ctrl.GetConfigOrDie())

	if n.gates.Enabled(features.ProxyClusterScoped) {
		apis, err := serverPreferredResources(discoveryClient)
		if err != nil {
			panic(err)
		}

		for _, api := range apis {
			if !moduleGroupKindPresent(modList, api) {
				n.log.V(6).Info("adding generic cluster scoped resource", "url", api.Path())
				modList = append(modList, clusterscoped.List(n.reader, n.writer, api.Path()))
				modList = append(modList, clusterscoped.Get(discoveryClient, n.reader, n.writer, api.ResourcePath()))
			}
		}
	} else {
		// Adds all legacy routes
		modList = append(modList, []modules.Module{
			node.List(n.reader),
			node.Get(n.reader),
			ingressclass.List(n.reader),
			ingressclass.Get(n.reader),
			storageclass.Get(n.reader),
			storageclass.List(n.reader),
			priorityclass.List(n.reader),
			priorityclass.Get(n.reader),
			runtimeclass.Get(n.reader),
			runtimeclass.List(n.reader),
			persistentvolume.Get(n.reader),
			persistentvolume.List(n.reader),
			metric.Get(n.reader),
			metric.List(n.reader),
		}...,
		)
	}

	// Get all API group resources
	apis, err := discoverAPI(ctrl.GetConfigOrDie())
	if err != nil {
		panic(err)
	}

	n.namespacedResources = sets.New[string]()

	for _, api := range apis {
		n.log.V(6).Info("adding generic namespaced resource", "url", api.Path())
		modList = append(modList, namespaced.CatchAll(
			n.writer,
			n.roleBindingsReflector,
			api.Path(),
			api.Group,
			api.Version,
			api.URLName,
		))
		n.namespacedResources.Insert(authorization.NamespacedResourceKey(api.Group, api.URLName))
	}

	for _, i := range modList {
		mod := i
		rp := root.Path(mod.Path())

		if m := mod.Methods(); len(m) > 0 {
			rp = rp.Methods(m...)
		}

		sr := rp.Subrouter()
		sr.Use(
			middleware.CheckPaths(n.log, n.allowedPaths, n.impersonateHandler),
			middleware.CheckJWTMiddleware(n.writer),
			middleware.CheckUserInIgnoredGroupMiddleware(n.writer, n.log, n.usernameClaimField, n.authTypes, n.ignoredUserGroups, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header, n.impersonateHandler),
			middleware.CheckUserInCapsuleGroupMiddleware(n.writer, n.log, n.usernameClaimField, n.authTypes, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header, n.impersonateHandler),
		)
		sr.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
			request, username, groups, err := req.ResolveUserAndGroups(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.xfcc_header)
			if err != nil {
				n.handleResolveUserAndGroupsError(writer, err)

				return
			}

			proxyTenants, err := n.getTenantsForOwner(ctx, username, groups)
			if err != nil {
				server.HandleError(writer, err, "cannot list Tenant resources")

				return
			}

			var selector labels.Selector

			selector, err = mod.Handle(
				proxyTenants,
				req.NewHTTP(
					request,
					n.authTypes,
					n.usernameClaimField,
					n.writer,
					n.ignoredImpersonationGroups,
					n.impersonationGroupsRegexp,
					n.skipImpersonationReview,
					n.xfcc_header,
				))

			switch {
			case err != nil:
				var t moderrors.Error
				if errors.As(err, &t) {
					writer.Header().Set("Content-Type", "application/json")

					if t.Status().Code > 0 {
						writer.WriteHeader(int(t.Status().Code))
					} else {
						writer.WriteHeader(http.StatusInternalServerError)
					}

					b, _ := json.Marshal(t.Status())
					_, _ = writer.Write(b)

					return
				}

				server.HandleError(writer, err, err.Error())

				return
			case selector == nil:
				// if there's no selector, let it pass to the
				n.impersonateHandler(writer, request)
			default:
				n.handleRequest(request, selector)
			}
		})
	}
}

func (n *kubeFilter) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(err)
			}

			n.log.Error(fmt.Errorf("%v", recovered), "panic while handling request")
			server.HandleError(writer, fmt.Errorf("internal server error"), "panic while handling request")
		}()

		next.ServeHTTP(writer, request)
	})
}

func (n *kubeFilter) handleResolveUserAndGroupsError(writer http.ResponseWriter, err error) {
	var unauthorizedErr *req.ErrUnauthorized
	if errors.As(err, &unauthorizedErr) {
		server.HandleUnauthorized(writer, err, "cannot retrieve user and group from the request")

		return
	}

	server.HandleError(writer, err, "cannot retrieve user and group from the request")
}

func (n *kubeFilter) getTenantsForOwner(ctx context.Context, username string, groups []string) (proxyTenants []*tenant.ProxyTenant, err error) {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(ctx, capsulerbac.ServiceAccountOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	} else {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(ctx, capsulerbac.UserOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	}

	// Find tenants belonging to a group
	for _, group := range groups {
		pt, err := n.getProxyTenantsForOwnerKind(ctx, capsulerbac.GroupOwner, group)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}

		proxyTenants = append(proxyTenants, pt...)
	}

	return
}

//nolint:funlen
func (n *kubeFilter) getProxyTenantsForOwnerKind(ctx context.Context, ownerKind capsulerbac.OwnerKind, ownerName string) (proxyTenants []*tenant.ProxyTenant, err error) {
	ownerIndexValue := fmt.Sprintf("%s:%s", ownerKind.String(), ownerName)

	tl := &capsulev1beta2.TenantList{}

	f := client.MatchingFields{
		indexer.TenantOwnerKindField: ownerIndexValue,
	}
	if err = n.managerReader.List(ctx, tl, f); err != nil {
		return nil, fmt.Errorf("cannot retrieve Tenants list: %w", err)
	}

	n.log.V(8).Info("Tenant", "owner", ownerKind, "name", ownerName, "tenantList items", tl.Items, "number of tenants", len(tl.Items))

	proxySettings := &v1beta1.ProxySettingList{}
	if err = n.managerReader.List(ctx, proxySettings, client.MatchingFields{indexer.SubjectKindField: ownerIndexValue}); err != nil {
		n.log.Error(err, "cannot retrieve ProxySetting", "owner", ownerKind, "name", ownerName)
	}

	n.log.V(10).Info("Collected ProxySettings", "owner", ownerKind, "name", ownerName, "settings", proxySettings)

	for _, proxySetting := range proxySettings.Items {
		tntList := &capsulev1beta2.TenantList{}
		if err = n.managerReader.List(ctx, tntList, client.MatchingFields{".status.namespaces": proxySetting.GetNamespace()}); err != nil {
			n.log.Error(err, "cannot retrieve Tenant list for ProxySetting", "owner", ownerKind, "name", ownerName)

			continue
		}

		if len(tntList.Items) == 0 {
			continue
		}

		proxyTenants = append(proxyTenants, tenant.NewProxyTenant(tntList.Items[0], ownerName, ownerKind, proxySetting.Spec.Subjects, n.gates.Enabled(features.ProxyClusterScoped)))
	}

	// Consider Global ProxySettings
	if n.gates.Enabled(features.ProxyClusterScoped) {
		globalProxySettings := &v1beta1.GlobalProxySettingsList{}
		if err = n.managerReader.List(ctx, globalProxySettings, client.MatchingFields{indexer.GlobalKindField: ownerIndexValue}); err != nil {
			n.log.Error(err, "cannot retrieve GlobalProxySettings", "owner", ownerKind, "name", ownerName)
		}
		// Convert GlobalProxySettings to TenantProxies
		for _, globalProxySetting := range globalProxySettings.Items {
			n.log.V(10).Info("Converting GlobalProxySettings", "Setting", globalProxySetting.Name)

			tProxy := tenant.NewClusterProxy(ownerName, ownerKind, globalProxySetting.Spec.Rules)
			proxyTenants = append(proxyTenants, tProxy)
		}

		n.log.V(10).Info("Collected GlobalProxySettings", "owner", ownerKind, "name", ownerName, "settings", len(globalProxySettings.Items))
	}

	tenants := make([]string, 0, len(tl.Items))

	for _, t := range tl.Items {
		proxyTenants = append(proxyTenants, tenant.NewProxyTenant(t, ownerName, ownerKind, n.ownerFromCapsuleToProxySetting(t.Spec.Owners), n.gates.Enabled(features.ProxyClusterScoped)))
		tenants = append(tenants, t.GetName())
	}

	n.log.V(4).Info("Proxy tenant list", "owner", ownerKind, "name", ownerName, "tenants", tenants)

	return proxyTenants, nil
}

func (n *kubeFilter) removingHopByHopHeaders(request *http.Request) {
	connectionHeaderName, upgradeHeaderName, requestUpgradeType := "connection", "upgrade", ""

	if httpguts.HeaderValuesContainsToken(request.Header.Values(connectionHeaderName), upgradeHeaderName) {
		requestUpgradeType = request.Header.Get(upgradeHeaderName)
	}
	// Removing connection headers
	for _, f := range request.Header.Values(connectionHeaderName) {
		for sf := range strings.SplitSeq(f, ",") {
			if sf = textproto.TrimString(sf); sf != "" {
				request.Header.Del(sf)
			}
		}
	}

	if requestUpgradeType != "" {
		request.Header.Set(connectionHeaderName, upgradeHeaderName)
		request.Header.Set(upgradeHeaderName, requestUpgradeType)

		return
	}

	request.Header.Del(connectionHeaderName)
}

func bearerExpirationTime(tokenString string) time.Time {
	token, _, _ := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	claims, _ := token.Claims.(jwt.MapClaims)

	var mil int64

	switch iat := claims["exp"].(type) {
	case float64:
		mil = int64(iat)
	case json.Number:
		mil, _ = iat.Int64()
	}

	return time.Unix(mil, 0)
}
