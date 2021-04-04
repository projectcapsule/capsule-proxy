package webserver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/utils"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	v1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-proxy/internal/options"
	req "github.com/clastix/capsule-proxy/internal/request"
)

var (
	log = ctrl.Log.WithName("namespace_filter")
)

func NewKubeFilter(opts options.ListenerOpts, srv options.ServerOptions) (Filter, error) {
	reverseProxy := httputil.NewSingleHostReverseProxy(opts.KubernetesControlPlaneURL())
	reverseProxy.FlushInterval = time.Millisecond * 100

	reverseProxyTransport, err := opts.ReverseProxyTransport()
	if err != nil {
		return nil, err
	}
	reverseProxy.Transport = reverseProxyTransport

	return &kubeFilter{
		capsuleUserGroup:   opts.UserGroupName(),
		reverseProxy:       reverseProxy,
		bearerToken:        opts.BearerToken(),
		usernameClaimField: opts.PreferredUsernameClaim(),
		serverOptions:      srv,
	}, nil
}

type kubeFilter struct {
	capsuleUserGroup   string
	reverseProxy       *httputil.ReverseProxy
	client             client.Client
	bearerToken        string
	usernameClaimField string
	serverOptions      options.ServerOptions
}

func (n *kubeFilter) LivenessProbe(req *http.Request) error {
	return nil
}

func (n *kubeFilter) ReadinessProbe(req *http.Request) error {
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

	resp, err := clt.Get(fmt.Sprintf("%s://localhost:%d/_healthz", scheme, n.serverOptions.ListeningPort()))
	if err != nil {
		return fmt.Errorf("cannot make local _healthz request: %s", err.Error())
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

func (n kubeFilter) checkUserInCapsuleGroupMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, groups, err := req.NewHTTP(request, n.usernameClaimField).GetUserAndGroups()
		if err != nil {
			log.Error(err, "Cannot retrieve username and group from request")
		}
		if utils.UserGroupList(groups).IsInCapsuleGroup(n.capsuleUserGroup) {
			next.ServeHTTP(writer, request)
			return
		}
		log.V(5).Info("current user is not a Capsule one")
		n.impersonateHandler(writer, request)
	})
}

func (n kubeFilter) checkJWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var err error

		token := strings.ReplaceAll(request.Header.Get("Authorization"), "Bearer ", "")

		if len(token) > 0 {
			log.V(4).Info("Checking JWT token", "value", token)
			tr := &v1.TokenReview{
				Spec: v1.TokenReviewSpec{
					Token: token,
				},
			}
			if err = n.client.Create(context.Background(), tr); err != nil {
				handleError(writer, err, "cannot create TokenReview")
			}
			log.V(5).Info("TokenReview", "value", tr.String())
			if statusErr := tr.Status.Error; len(statusErr) > 0 {
				handleError(writer, err, "cannot verify the token due to error")
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func (n kubeFilter) reverseProxyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		next.ServeHTTP(writer, request)

		log.V(5).Info("debugging request", "uri", request.RequestURI, "method", request.Method)
		n.reverseProxy.ServeHTTP(writer, request)
	})
}

// nolint:interfacer
func (n kubeFilter) handleRequest(request *http.Request, username string, selector labels.Selector) {
	q := request.URL.Query()
	if e := q.Get("labelSelector"); len(e) > 0 {
		log.V(4).Info("handling current labelSelector", "selector", e)
		if err := n.validateCapsuleLabel(e, username); err != nil {
			log.Error(err, "cannot validate Capsule label selector, ignoring it")
			panic(err)
		}
		v := strings.Join([]string{e, selector.String()}, ",")
		q.Set("labelSelector", v)
		log.V(4).Info("labelSelector updated", "selector", v)
	} else {
		q.Set("labelSelector", selector.String())
		log.V(4).Info("labelSelector added", "selector", selector.String())
	}
	log.V(4).Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	if len(n.bearerToken) > 0 {
		log.V(4).Info("Updating the token", "token", n.bearerToken)
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
	}
}

func (n kubeFilter) namespacesHandler(writer http.ResponseWriter, request *http.Request) {
	log.V(2).Info("Decorating request for Namespace filtering")

	username, groups, _ := req.NewHTTP(request, n.usernameClaimField).GetUserAndGroups()
	log.V(4).Info("Getting user from request", "username", username, "groups", groups)

	selector, err := n.getLabelSelectorForOwner(username, groups, nil)
	if err != nil {
		handleError(writer, err, "cannot create label selector")
	}

	n.handleRequest(request, username, selector)
}

func (n kubeFilter) impersonateHandler(writer http.ResponseWriter, request *http.Request) {
	if n.serverOptions.IsListeningTLS() {
		log.V(3).Info("running on TLS, need to check the certificate")

		if pc := request.TLS.PeerCertificates; len(pc) == 1 {
			hr := req.NewHTTP(request, n.usernameClaimField)
			username, groups, err := hr.GetUserAndGroups()
			if err != nil {
				handleError(writer, err, "Cannot retrieve user and group from Request certificate")
			}
			log.V(4).Info("Impersonating for the current request", "username", username, "groups", groups, "token", n.bearerToken)
			if len(n.bearerToken) > 0 {
				request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
			}
			request.Header.Add("Impersonate-User", username)
			request.Header.Add("Impersonate-Group", strings.Join(groups, ","))
		}
	}
}

func (n kubeFilter) Start(ctx context.Context) error {
	r := mux.NewRouter()
	r.StrictSlash(true)
	r.Use(handlers.RecoveryHandler())

	h := r.Path("/_healthz").Subrouter()
	h.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		_, _ = writer.Write([]byte("ok"))
	})

	root := r.PathPrefix("").Subrouter()
	root.Use(n.reverseProxyMiddleware)

	ns := root.Path("/api/v1/namespaces").Subrouter()
	ns.Use(n.checkJWTMiddleware, n.checkUserInCapsuleGroupMiddleware)
	ns.HandleFunc("", func(writer http.ResponseWriter, request *http.Request) {
		n.namespacesHandler(writer, request)
	})

	n.registerNode(root)

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

func (n *kubeFilter) getTenantsForOwner(username string, groups []string) (tenants *capsulev1alpha1.TenantList, err error) {
	ownedTenants, _, err := n.getTenantsForOwnerKind("User", username)
	if err != nil {
		return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %s", err.Error())
	}
	// Find tenants belonging to a group
	for _, group := range groups {
		t, _, err := n.getTenantsForOwnerKind("Group", group)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %s", err.Error())
		}
		ownedTenants.Items = append(ownedTenants.Items, t.Items...)
	}
	return ownedTenants, nil
}

func (n kubeFilter) getTenantsForOwnerKind(ownerKind string, ownerName string) (tl *capsulev1alpha1.TenantList, tenants []string, err error) {
	tl = &capsulev1alpha1.TenantList{}

	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	if err := n.client.List(context.Background(), tl, f); err != nil {
		return nil, nil, fmt.Errorf("cannot retrieve Tenants list: %s", err.Error())
	}

	for _, t := range tl.Items {
		tenants = append(tenants, t.GetName())
	}

	log.V(4).Info("Tenants list", "owner", ownerKind, "name", ownerName, "tenants", tenants)
	return
}

func (n kubeFilter) getLabelSelectorForOwner(username string, groups []string, filter func(*capsulev1alpha1.TenantList) *capsulev1alpha1.TenantList) (labels.Selector, error) {
	var ownedTenants []string

	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return nil, fmt.Errorf("cannot get Capsule Tenant label: %s", err.Error())
	}

	tenantList, err := n.getTenantsForOwner(username, groups)
	if err != nil {
		return nil, err
	}

	if filter != nil {
		tenantList = filter(tenantList)
	}

	for _, t := range tenantList.Items {
		ownedTenants = append(ownedTenants, t.GetName())
	}

	var r *labels.Requirement
	if len(ownedTenants) > 0 {
		r, err = labels.NewRequirement(capsuleLabel, selection.In, ownedTenants)
	} else {
		r, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}
	if err != nil {
		return nil, fmt.Errorf("cannot parse Tenant selector: %s", err.Error())
	}
	return labels.NewSelector().Add(*r), nil
}

// We have to validate User requesting labels since we're changing the Authorization Bearer since the Tenant Owner
// does not have permission to list Namespaces: in case of filtering by non-owned namespaces, we have to return an
// error, otherwise everything is good.
func (n kubeFilter) validateCapsuleLabel(value, username string) error {
	p, err := labels.Parse(value)
	if err != nil {
		// we're ignoring this, API Server will deal with this
		return nil
	}

	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return fmt.Errorf("cannot get Capsule Tenant label: %s", err.Error())
	}

	r, selectable := p.Requirements()
	if !selectable {
		return nil
	}
	for _, i := range r {
		if i.Key() == capsuleLabel {
			_, tenants, _ := n.getTenantsForOwnerKind("User", username)
			switch i.Operator() {
			case selection.Exists:
				return fmt.Errorf("cannot return list of all Tenant namespaces")
			case selection.In:
				if ss := i.Values().Delete(tenants...); ss.Len() > 0 {
					return fmt.Errorf("cannot list Namespaces for the following Tenant(s): %s", strings.Join(ss.List(), ", "))
				}
			default:
				break
			}
		}
	}
	return nil
}
