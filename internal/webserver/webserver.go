// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package webserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	"github.com/go-logr/logr"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/controllers"
	"github.com/clastix/capsule-proxy/internal/modules"
	moderrors "github.com/clastix/capsule-proxy/internal/modules/errors"
	"github.com/clastix/capsule-proxy/internal/modules/ingressclass"
	"github.com/clastix/capsule-proxy/internal/modules/lease"
	"github.com/clastix/capsule-proxy/internal/modules/metric"
	"github.com/clastix/capsule-proxy/internal/modules/namespace"
	"github.com/clastix/capsule-proxy/internal/modules/node"
	"github.com/clastix/capsule-proxy/internal/modules/pod"
	"github.com/clastix/capsule-proxy/internal/modules/priorityclass"
	"github.com/clastix/capsule-proxy/internal/modules/storageclass"
	"github.com/clastix/capsule-proxy/internal/options"
	req "github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/tenant"
	serverr "github.com/clastix/capsule-proxy/internal/webserver/errors"
	"github.com/clastix/capsule-proxy/internal/webserver/middleware"
)

func NewKubeFilter(opts options.ListenerOpts, srv options.ServerOptions, rbReflector *controllers.RoleBindingReflector) (Filter, error) {
	reverseProxy := httputil.NewSingleHostReverseProxy(opts.KubernetesControlPlaneURL())
	reverseProxy.FlushInterval = time.Millisecond * 100

	reverseProxyTransport, err := opts.ReverseProxyTransport()
	if err != nil {
		return nil, errors.Wrap(err, "cannot create transport for reverse proxy")
	}

	reverseProxy.Transport = reverseProxyTransport

	return &kubeFilter{
		allowedPaths:          sets.NewString("/api", "/apis", "/version"),
		ignoredUserGroups:     sets.NewString(opts.IgnoredGroupNames()...),
		reverseProxy:          reverseProxy,
		bearerToken:           opts.BearerToken(),
		usernameClaimField:    opts.PreferredUsernameClaim(),
		serverOptions:         srv,
		log:                   ctrl.Log.WithName("proxy"),
		roleBindingsReflector: rbReflector,
	}, nil
}

type kubeFilter struct {
	allowedPaths          sets.String
	ignoredUserGroups     sets.String
	reverseProxy          *httputil.ReverseProxy
	client                client.Client
	bearerToken           string
	usernameClaimField    string
	serverOptions         options.ServerOptions
	log                   logr.Logger
	roleBindingsReflector *controllers.RoleBindingReflector
}

func (n *kubeFilter) LivenessProbe(req *http.Request) error {
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

	if r, err = http.NewRequestWithContext(context.Background(), "GET", url, nil); err != nil {
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

func (n *kubeFilter) InjectClient(client client.Client) error {
	n.client = client

	return nil
}

func (n kubeFilter) reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request)

		n.log.V(5).Info("debugging request", "uri", request.RequestURI, "method", request.Method)
		n.reverseProxy.ServeHTTP(writer, request)
	})
}

// nolint:interfacer
func (n kubeFilter) handleRequest(request *http.Request, selector labels.Selector) {
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

	if len(n.bearerToken) > 0 {
		n.log.V(4).Info("Updating the token", "token", n.bearerToken)
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
	}
}

func (n kubeFilter) impersonateHandler(writer http.ResponseWriter, request *http.Request) {
	hr := req.NewHTTP(request, n.usernameClaimField, n.client)

	var username string

	var groups []string

	var err error

	if username, groups, err = hr.GetUserAndGroups(); err != nil {
		serverr.HandleError(writer, err, "cannot retrieve user and group")
	}

	n.log.V(4).Info("impersonating for the current request", "username", username, "groups", groups)

	if len(n.bearerToken) > 0 {
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
	}

	request.Header.Add("Impersonate-User", username)

	for _, group := range groups {
		request.Header.Add("Impersonate-Group", group)
	}
}

func (n kubeFilter) registerModules(root *mux.Router) {
	modList := []modules.Module{
		namespace.List(n.roleBindingsReflector),
		node.List(n.client),
		node.Get(n.client),
		ingressclass.List(n.client),
		ingressclass.Get(n.client),
		storageclass.Get(n.client),
		storageclass.List(n.client),
		priorityclass.List(n.client),
		priorityclass.Get(n.client),
		lease.Get(n.client),
		metric.Get(n.client),
		metric.List(n.client),
		pod.Get(n.client),
	}
	for _, i := range modList {
		mod := i
		rp := root.Path(mod.Path())

		if m := mod.Methods(); len(m) > 0 {
			rp = rp.Methods(m...)
		}

		sr := rp.Subrouter()
		sr.Use(
			middleware.CheckPaths(n.client, n.log, n.allowedPaths, n.impersonateHandler),
			middleware.CheckAuthorization(n.client, n.log, n.serverOptions.IsListeningTLS()),
			middleware.CheckJWTMiddleware(n.client, n.log),
			middleware.CheckUserInIgnoredGroupMiddleware(n.client, n.log, n.usernameClaimField, n.ignoredUserGroups, n.impersonateHandler),
			middleware.CheckUserInCapsuleGroupMiddleware(n.client, n.log, n.usernameClaimField, n.impersonateHandler),
		)
		sr.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
			proxyRequest := req.NewHTTP(request, n.usernameClaimField, n.client)
			username, groups, _ := proxyRequest.GetUserAndGroups()
			proxyTenants, err := n.getTenantsForOwner(username, groups)
			if err != nil {
				serverr.HandleError(writer, err, "cannot list Tenant resources")
			}

			var selector labels.Selector
			selector, err = mod.Handle(proxyTenants, proxyRequest)
			switch {
			case err != nil:
				var t moderrors.Error
				if errors.As(err, &t) {
					writer.Header().Set("content-type", "application/json")
					b, _ := json.Marshal(t.Status())
					_, _ = writer.Write(b)
					panic(err.Error())
				}
				serverr.HandleError(writer, err, err.Error())
			case selector == nil:
				// if there's no selector, let it pass to the
				n.impersonateHandler(writer, request)
			default:
				n.handleRequest(request, selector)
			}
		})
	}
}

func (n kubeFilter) Start(ctx context.Context) error {
	r := mux.NewRouter().StrictSlash(true)
	r.Use(handlers.RecoveryHandler())
	r.Path("/_healthz").Subrouter().HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		_, _ = writer.Write([]byte("ok"))
	})

	root := r.PathPrefix("").Subrouter()
	n.registerModules(root)
	root.Use(
		n.reverseProxyMiddleware,
		middleware.CheckPaths(n.client, n.log, n.allowedPaths, n.impersonateHandler),
		middleware.CheckAuthorization(n.client, n.log, n.serverOptions.IsListeningTLS()),
		middleware.CheckJWTMiddleware(n.client, n.log),
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
				ClientAuth: tls.VerifyClientCertIfGiven,
			}
			srv = &http.Server{
				Handler:   r,
				Addr:      addr,
				TLSConfig: tlsConfig,
			}
			err = srv.ListenAndServeTLS(n.serverOptions.TLSCertificatePath(), n.serverOptions.TLSCertificateKeyPath())
		} else {
			srv = &http.Server{
				Handler: r,
				Addr:    addr,
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

func (n *kubeFilter) getTenantsForOwner(username string, groups []string) (proxyTenants []*tenant.ProxyTenant, err error) {
	if strings.HasPrefix(username, serviceaccount.ServiceAccountUsernamePrefix) {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(capsulev1beta1.ServiceAccountOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	} else {
		proxyTenants, err = n.getProxyTenantsForOwnerKind(capsulev1beta1.UserOwner, username)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}
	}

	// Find tenants belonging to a group
	for _, group := range groups {
		pt, err := n.getProxyTenantsForOwnerKind(capsulev1beta1.GroupOwner, group)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %w", err)
		}

		proxyTenants = append(proxyTenants, pt...)
	}

	return
}

func (n kubeFilter) getProxyTenantsForOwnerKind(ownerKind capsulev1beta1.OwnerKind, ownerName string) (proxyTenants []*tenant.ProxyTenant, err error) {
	// nolint:prealloc
	var tenants []string

	tl := &capsulev1beta1.TenantList{}

	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind.String(), ownerName),
	}
	if err := n.client.List(context.Background(), tl, f); err != nil {
		return nil, fmt.Errorf("cannot retrieve Tenants list: %w", err)
	}

	n.log.V(8).Info("Tenant", "owner", ownerKind, "name", ownerName, "tenantList items", tl.Items, "number of tenants", len(tl.Items))

	for _, t := range tl.Items {
		proxyTenants = append(proxyTenants, tenant.NewProxyTenant(ownerName, ownerKind, t))
		tenants = append(tenants, t.GetName())
	}

	n.log.V(4).Info("Proxy tenant list", "owner", ownerKind, "name", ownerName, "tenants", tenants)

	return
}
