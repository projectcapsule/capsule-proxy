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

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/utils"
	v1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule-ns-filter/internal/options"
	req "github.com/clastix/capsule-ns-filter/internal/request"
)

var (
	log = ctrl.Log.WithName("namespace_filter")
)

func NewKubeFilter(opts options.ListenerOpts, srv options.ServerOptions) (Filter, error) {
	reverseProxy := httputil.NewSingleHostReverseProxy(opts.KubernetesControlPlaneUrl())
	reverseProxy.FlushInterval = time.Millisecond * 100
	reverseProxy.Transport = opts.ReverseProxyTransport()

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

	if n.serverOptions.IsListeningTls() {
		scheme = "https"
		clt = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
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

func (n kubeFilter) checkJwt(fn http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		var err error

		token := strings.Replace(request.Header.Get("Authorization"), "Bearer ", "", -1)

		if len(token) > 0 {
			log.V(4).Info("Checking JWT token", "value", token)
			tr := &v1.TokenReview{
				Spec: v1.TokenReviewSpec{
					Token: token,
				},
			}
			if err := n.client.Create(context.Background(), tr); err != nil {
				log.Error(err, "cannot create TokenReview")
				n.handleError(err, writer)
				return
			}
			log.V(5).Info("TokenReview", "value", tr.String())
			if statusErr := tr.Status.Error; len(statusErr) > 0 {
				err = fmt.Errorf("cannot verify the token due to error")
				log.Error(err, statusErr)
				n.handleError(err, writer)
				return
			}
		}
		fn(writer, request)
	}
}

func (n kubeFilter) namespacesHandler(writer http.ResponseWriter, request *http.Request) {
	log.V(2).Info("Decorating request for Namespace filtering")

	r := req.NewHttp(request, n.usernameClaimField)

	var err error
	username, groups, err := r.GetUserAndGroups()

	// breaking for non-Capsule user
	if !utils.UserGroupList(groups).IsInCapsuleGroup(n.capsuleUserGroup) {
		log.V(5).Info("current user is not a Capsule one")
		return
	}

	log.V(4).Info("Getting user from request", "username", username, "groups", groups)

	var s labels.Selector
	s, err = n.getLabelSelectorForOwner(username, groups)
	if err != nil {
		log.Error(err, "cannot create label selector")
		panic(err)
	}

	q := request.URL.Query()
	if e := q.Get("labelSelector"); len(e) > 0 {
		log.V(4).Info("handling current labelSelector", "selector", e)
		if err := n.validateCapsuleLabel(e, username); err != nil {
			log.Error(err, "cannot validate Capsule label selector, ignoring it")
			panic(err)
		}
		v := strings.Join([]string{e, s.String()}, ",")
		q.Set("labelSelector", v)
		log.V(4).Info("labelSelector updated", "selector", v)
	} else {
		q.Set("labelSelector", s.String())
		log.V(4).Info("labelSelector added", "selector", s.String())
	}
	log.V(4).Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	log.V(4).Info("Updating the token", "token", n.bearerToken)
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
}

func (n kubeFilter) isNamespaceListing(fn http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if req.NewHttp(request, n.usernameClaimField).IsNamespaceListing() {
			log.V(3).Info("Handling /api/v1/namespaces")
			fn(writer, request)
		}
	}
}

func (n kubeFilter) Start(ctx context.Context) error {
	http.HandleFunc("/api/v1/namespaces", func(writer http.ResponseWriter, request *http.Request) {
		n.isNamespaceListing(n.checkJwt(n.namespacesHandler))(writer, request)
		n.reverseProxyFunc(writer, request)
	})
	http.HandleFunc("/_healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		_, _ = writer.Write([]byte("ok"))
	})
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		n.reverseProxyFunc(writer, request)
	})

	var srv *http.Server

	go func() {
		var err error

		addr := fmt.Sprintf("0.0.0.0:%d", n.serverOptions.ListeningPort())

		if n.serverOptions.IsListeningTls() {
			tlsConfig := &tls.Config{
				ClientCAs:  n.serverOptions.GetCertificateAuthorityPool(),
				ClientAuth: tls.VerifyClientCertIfGiven,
			}
			srv = &http.Server{
				Addr:      addr,
				TLSConfig: tlsConfig,
			}
			err = srv.ListenAndServeTLS(n.serverOptions.TlsCertificatePath(), n.serverOptions.TlsCertificateKeyPath())
		} else {
			srv = &http.Server{Addr: addr}
			err = srv.ListenAndServe()
		}
		if err != nil {
			panic(err)
		}
	}()

	<-ctx.Done()

	return srv.Shutdown(ctx)
}

func (n kubeFilter) reverseProxyFunc(writer http.ResponseWriter, request *http.Request) {
	if n.serverOptions.IsListeningTls() {
		log.V(3).Info("running on TLS, need to check the certificate")

		if pc := request.TLS.PeerCertificates; len(pc) == 1 {
			r := req.NewHttp(request, n.usernameClaimField)
			username, groups, err := r.GetUserAndGroups()
			if err == nil {
				log.V(4).Info("Impersonating for the current request", "username", username, "groups", groups, "token", n.bearerToken)
				request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))
				request.Header.Add("Impersonate-User", username)
				request.Header.Add("Impersonate-Group", strings.Join(groups, ","))
			}
		}
	}
	log.V(5).Info("debugging request", "uri", request.RequestURI, "method", request.Method)
	n.reverseProxy.ServeHTTP(writer, request)
}

type errorJSON struct {
	Error string `json:"error"`
}

func (n kubeFilter) handleError(err error, writer http.ResponseWriter) {
	log.Error(err, "handling failed request")
	writer.WriteHeader(500)
	writer.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(errorJSON{Error: err.Error()})
	_, _ = writer.Write(b)
}

func (n kubeFilter) getTenantsForOwner(ownerKind string, ownerName string) (tenants []string, err error) {
	tl := &capsulev1alpha1.TenantList{}
	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", ownerKind, ownerName),
	}
	if err := n.client.List(context.Background(), tl, f); err != nil {
		return nil, fmt.Errorf("cannot retrieve Tenants list: %s", err.Error())
	}
	for _, t := range tl.Items {
		tenants = append(tenants, t.GetName())
	}
	log.V(4).Info("Tenants list", "owner", ownerKind, "name", ownerName, "tenants", tenants)
	return
}

func (n kubeFilter) getLabelSelectorForOwner(username string, groups []string) (labels.Selector, error) {
	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return nil, fmt.Errorf("cannot get Capsule Tenant label: %s", err.Error())
	}
	// Find tenants belonging to a user
	ownedTenants, err := n.getTenantsForOwner("User", username)
	if err != nil {
		return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %s", err.Error())
	}
	// Find tenants belonging to a group
	for _, group := range groups {
		t, err := n.getTenantsForOwner("Group", group)
		if err != nil {
			return nil, fmt.Errorf("cannot get Tenants slice owned by Tenant Owner: %s", err.Error())
		}
		ownedTenants = append(ownedTenants, t...)
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
			tenants, _ := n.getTenantsForOwner("User", username)
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
