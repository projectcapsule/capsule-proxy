// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/textproto"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/projectcapsule/capsule-proxy/internal/authorization"
	"github.com/projectcapsule/capsule-proxy/internal/utils"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"golang.org/x/net/http/httpguts"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"k8s.io/client-go/discovery"
	"k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/controllers/watchdog"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/modules"
	"github.com/projectcapsule/capsule-proxy/internal/modules/clusterscoped"
	moderrors "github.com/projectcapsule/capsule-proxy/internal/modules/errors"
	"github.com/projectcapsule/capsule-proxy/internal/modules/ingressclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/lease"
	"github.com/projectcapsule/capsule-proxy/internal/modules/metric"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespace"
	"github.com/projectcapsule/capsule-proxy/internal/modules/namespaced"
	"github.com/projectcapsule/capsule-proxy/internal/modules/node"
	"github.com/projectcapsule/capsule-proxy/internal/modules/persistentvolume"
	"github.com/projectcapsule/capsule-proxy/internal/modules/pod"
	"github.com/projectcapsule/capsule-proxy/internal/modules/priorityclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/runtimeclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/storageclass"
	"github.com/projectcapsule/capsule-proxy/internal/modules/tenants"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	req "github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/tenant"
	server "github.com/projectcapsule/capsule-proxy/internal/webserver/errors"
	"github.com/projectcapsule/capsule-proxy/internal/webserver/middleware"
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
		return nil, errors.Wrap(err, "cannot create transport for reverse proxy")
	}

	reverseProxy.Transport = reverseProxyTransport

	return &kubeFilter{
		mgr:                        mgr,
		gates:                      gates,
		reader:                     clientOverride,
		writer:                     mgr.GetClient(),
		managerReader:              mgr.GetClient(),
		allowedPaths:               sets.New("/api", "/apis", "/version"),
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
	}, nil
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
	gates                      featuregate.FeatureGate

	managerReader, reader client.Reader
	writer                client.Writer
}

// NeedLeaderElection starts the proxy (webserver) independently of controller manager
// This allows distributing the load among all pods, even if they are not leaders.
func (n *kubeFilter) NeedLeaderElection() bool {
	return false
}

//nolint:funlen
func (n *kubeFilter) Start(ctx context.Context) error {
	r := mux.NewRouter()
	r.Use(handlers.RecoveryHandler())

	r.Path("/_healthz").Subrouter().HandleFunc("", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	})

	root := r.PathPrefix("").Subrouter()
	n.registerModules(ctx, root)
	root.Use(
		n.AuthorizationMiddleware,
		n.reverseProxyMiddleware,
		middleware.CheckPaths(n.log, n.allowedPaths, n.impersonateHandler),
		middleware.CheckJWTMiddleware(n.writer),
	)
	root.PathPrefix("/").HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		n.impersonateHandler(writer, request)
	})

	var srv *http.Server

	go func() {
		var err error

		addr := fmt.Sprintf("0.0.0.0:%d", n.serverOptions.ListeningPort())

		if n.serverOptions.IsListeningTLS() {
			tlsConfig := &tls.Config{
				MinVersion: tls.VersionTLS12,
				ClientCAs:  n.serverOptions.GetCertificateAuthorityPool(),
			}

			for _, authType := range n.authTypes {
				if authType == req.TLSCertificate {
					tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven

					break
				}
			}

			srv = &http.Server{
				Handler:           r,
				Addr:              addr,
				TLSConfig:         tlsConfig,
				ReadHeaderTimeout: 5 * time.Second,
			}
			err = srv.ListenAndServeTLS(n.serverOptions.TLSCertificatePath(), n.serverOptions.TLSCertificateKeyPath())
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
		return errors.Wrap(err, "cannot create request")
	}

	var resp *http.Response

	if resp, err = clt.Do(r); err != nil {
		return errors.Wrap(err, "cannot make local _healthz request")
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

func (n *kubeFilter) AuthorizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !slices.Contains(authorization.Paths, request.URL.Path) {
			next.ServeHTTP(writer, request)
			return
		}

		w := httptest.NewRecorder()
		next.ServeHTTP(w, request)
		body, err := io.ReadAll(w.Result().Body)
		if err != nil {
			n.log.Error(err, "cannot read response body")
			return
		}

		proxyRequest := req.NewHTTP(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview)

		username, groups, err := proxyRequest.GetUserAndGroups()
		if err != nil {
			server.HandleError(writer, err, "cannot retrieve user and group from the request")
		}

		proxyTenants, err := n.getTenantsForOwner(context.Background(), username, groups)
		if err != nil {
			server.HandleError(writer, err, "cannot list Tenant resources")
		}

		scheme := runtime.NewScheme()
		protoEncoder := protobuf.NewSerializer(scheme, scheme)
		corev1.AddToScheme(scheme)
		authorizationv1.AddToScheme(scheme)
		codecFactory := serializer.NewCodecFactory(scheme)
		universalDecoder := codecFactory.UniversalDeserializer()

		obj, gvk, err := universalDecoder.Decode(body, nil, nil)
		if err != nil {
			n.log.Error(err, "cannot decode authorization object")
		}
		err = authorization.MutateAuthorization(n.gates.Enabled(features.ProxyClusterScoped), proxyTenants, &obj, *gvk)
		if err != nil {
			n.log.Error(err, "cannot mutate authorization object")
		}
		if request.Header.Get("Content-Type") == "application/json" {
			body, err = utils.JsonEncode(obj, scheme)
			if err != nil {
				n.log.Error(err, "cannot marshal Authorization object to json")
			}
		} else if request.Header.Get("Content-Type") == "application/vnd.kubernetes.protobuf" {
			body, err = runtime.Encode(protoEncoder, obj)
			if err != nil {
				n.log.Error(err, "cannot marshal Authorization object to protobuf")
			}
		}
		for k, v := range w.Result().Header {
			if k == "Content-Length" {
				continue
			}
			for _, sv := range v {
				writer.Header().Add(k, sv)
			}
		}
		writer.WriteHeader(w.Result().StatusCode)
		writer.Write(body)
	})
}

//nolint:interfacer
func (n *kubeFilter) handleRequest(request *http.Request, selector labels.Selector) {
	req.SanitizeImpersonationHeaders(request)

	q := request.URL.Query()
	if e := q.Get("labelSelector"); len(e) > 0 {
		n.log.V(4).Info("handling current labelSelector", "selector", e)

		v := strings.Join([]string{e, selector.String()}, ",")
		q.Set("labelSelector", v)
		n.log.V(4).Info("labelSelector updated", "selector", v)
	} else {
		q.Set("labelSelector", selector.String())
		n.log.V(4).Info("labelSelector added", "selector", selector.String())
	}

	n.log.V(4).Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	if len(n.BearerToken()) > 0 {
		n.log.V(10).Info("Updating the token", "token", n.BearerToken())
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.BearerToken()))
	}
}

func (n *kubeFilter) impersonateHandler(writer http.ResponseWriter, request *http.Request) {
	hr := req.NewHTTP(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview)

	username, groups, err := hr.GetUserAndGroups()
	if err != nil {
		msg := "cannot retrieve user and group"

		var t *req.ErrUnauthorized
		if errors.As(err, &t) {
			server.HandleUnauthorized(writer, err, msg)
		} else {
			server.HandleError(writer, err, msg)
		}
	}

	n.log.V(4).Info("impersonating for the current request", "username", username, "groups", groups, "uri", request.URL.Path)

	if len(n.BearerToken()) > 0 {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.BearerToken()))
	}
	// Dropping malicious header connection
	// https://github.com/projectcapsule/capsule-proxy/issues/188
	n.removingHopByHopHeaders(request)

	request.Header.Add(authenticationv1.ImpersonateUserHeader, username)

	for _, group := range groups {
		request.Header.Add(authenticationv1.ImpersonateGroupHeader, group)
	}
}

//nolint:funlen
func (n *kubeFilter) registerModules(ctx context.Context, root *mux.Router) {
	// We are using namespaces and tenants as default routes from the legacy
	// system, as their outcome heavily relies on the tenants config/status
	modList := []modules.Module{
		namespace.Post(),
		namespace.List(n.roleBindingsReflector),
		namespace.Get(n.roleBindingsReflector, n.reader),
		tenants.List(),
		tenants.Get(n.reader),
	}

	// Discovery client
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(ctrl.GetConfigOrDie())

	// When the ProxyClusterScoped flag is enabled
	// we are no longer respecting legacy proxysettings
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
	if n.gates.Enabled(features.ProxyAllNamespaced) {
		apis, err := watchdog.API(ctrl.GetConfigOrDie())
		if err != nil {
			panic(err)
		}

		for _, api := range apis {
			n.log.V(6).Info("adding generic namespaced resource", "url", api.Path())
			modList = append(modList, namespaced.CatchAll(n.reader, n.writer, api.Path()))
		}
	} else {
		// Register legacy namespaced modules only when featureGate ProxyAllNamespaced is not active.
		// This is to avoid registering the same resources twice and having different behaviors on these apis.
		modList = append(modList, pod.Get(n.reader))
		modList = append(modList, lease.Get(n.reader))
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
			middleware.CheckUserInIgnoredGroupMiddleware(n.writer, n.log, n.usernameClaimField, n.authTypes, n.ignoredUserGroups, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.impersonateHandler),
			middleware.CheckUserInCapsuleGroupMiddleware(n.writer, n.log, n.usernameClaimField, n.authTypes, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview, n.impersonateHandler),
		)
		sr.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
			proxyRequest := req.NewHTTP(request, n.authTypes, n.usernameClaimField, n.writer, n.ignoredImpersonationGroups, n.impersonationGroupsRegexp, n.skipImpersonationReview)

			username, groups, err := proxyRequest.GetUserAndGroups()
			if err != nil {
				server.HandleError(writer, err, "cannot retrieve user and group from the request")
			}

			proxyTenants, err := n.getTenantsForOwner(ctx, username, groups)
			if err != nil {
				server.HandleError(writer, err, "cannot list Tenant resources")
			}

			var selector labels.Selector

			selector, err = mod.Handle(proxyTenants, proxyRequest)

			switch {
			case err != nil:
				var t moderrors.Error
				if errors.As(err, &t) {
					writer.Header().Set("Content-Type", "application/json")

					b, _ := json.Marshal(t.Status())
					_, _ = writer.Write(b)

					panic(err.Error())
				}

				server.HandleError(writer, err, err.Error())
			case selector == nil:
				// if there's no selector, let it pass to the
				n.impersonateHandler(writer, request)
			default:
				n.handleRequest(request, selector)
			}
		})
	}
}

func (n *kubeFilter) getTenantsForOwner(ctx context.Context, username string, groups []string) (proxyTenants []*tenant.ProxyTenant, err error) {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(ctx, capsulev1beta2.ServiceAccountOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	} else {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(ctx, capsulev1beta2.UserOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	}

	// Find tenants belonging to a group
	for _, group := range groups {
		pt, err := n.getProxyTenantsForOwnerKind(ctx, capsulev1beta2.GroupOwner, group)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}

		proxyTenants = append(proxyTenants, pt...)
	}

	return
}

func (n *kubeFilter) ownerFromCapsuleToProxySetting(owners capsulev1beta2.OwnerListSpec) []v1beta1.OwnerSpec {
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
func (n *kubeFilter) getProxyTenantsForOwnerKind(ctx context.Context, ownerKind capsulev1beta2.OwnerKind, ownerName string) (proxyTenants []*tenant.ProxyTenant, err error) {
	//nolint:prealloc
	var tenants []string

	ownerIndexValue := fmt.Sprintf("%s:%s", ownerKind.String(), ownerName)

	tl := &capsulev1beta2.TenantList{}

	f := client.MatchingFields{
		".spec.owner.ownerkind": ownerIndexValue,
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

		proxyTenants = append(proxyTenants, tenant.NewProxyTenant(ownerName, ownerKind, tntList.Items[0], proxySetting.Spec.Subjects))
	}

	// Consider Global ProxySettings
	// Only consider GlobalProxySettings if the feature gate is enabled
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

	for _, t := range tl.Items {
		proxyTenants = append(proxyTenants, tenant.NewProxyTenant(ownerName, ownerKind, t, n.ownerFromCapsuleToProxySetting(t.Spec.Owners)))
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
		for _, sf := range strings.Split(f, ",") {
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
