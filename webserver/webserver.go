package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	v1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	log = ctrl.Log.WithName("namespace_filter")
)

func NewKubeFilter(opts ListenerOptions, srv ServerOptions) (*kubeFilter, error) {

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
	serverOptions      ServerOptions
}

func (n *kubeFilter) InjectClient(client client.Client) error {
	n.client = client
	return nil
}

func (n kubeFilter) isWatchEndpoint(request *http.Request) (ok bool) {
	w, ok := request.URL.Query()["watch"]
	if ok && len(w) == 1 && w[0] == "true" {
		ok = true
	}
	return
}

func (n kubeFilter) checkJwt(fn func(writer http.ResponseWriter, request *http.Request)) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		var err error

		token := strings.Replace(request.Header.Get("Authorization"), "Bearer ", "", -1)

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
		if statusErr := tr.Status.Error; len(statusErr) > 0 {
			err = fmt.Errorf("cannot verify the token due to error")
			log.Error(err, statusErr)
			n.handleError(err, writer)
			return
		}
		fn(writer, request)
	}
}

func (n kubeFilter) Start(stop <-chan struct{}) error {
	http.HandleFunc("/api/v1/namespaces", n.checkJwt(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == "GET" || n.isWatchEndpoint(request) {
			log.V(2).Info("decorating /api/v1/namespaces request")
			if err := n.decorateRequest(writer, request); err != nil {
				n.handleError(err, writer)
				return
			}
		}
		n.reverseProxyFunc(writer, request)
	}))
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		n.reverseProxyFunc(writer, request)
	})
	http.HandleFunc("/_healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		_, _ = writer.Write([]byte("ok"))
	})

	go func() {
		addr := fmt.Sprintf("0.0.0.0:%d", n.serverOptions.ListeningPort())

		var err error
		if n.serverOptions.IsListeningTls() {
			err = http.ListenAndServeTLS(addr, n.serverOptions.TlsCertificatePath(), n.serverOptions.TlsCertificateKeyPath(), nil)
		} else {
			err = http.ListenAndServe(addr, nil)
		}
		if err != nil {
			panic(err)
		}
	}()

	<-stop

	return nil
}

func (n kubeFilter) reverseProxyFunc(writer http.ResponseWriter, request *http.Request) {
	log.V(2).Info("handling " + request.URL.String())
	n.reverseProxy.ServeHTTP(writer, request)
}

type errorJson struct {
	Error string `json:"error"`
}

func (n kubeFilter) handleError(err error, writer http.ResponseWriter) {
	log.Error(err, "handling failed request")
	writer.WriteHeader(500)
	writer.Header().Set("Content-Type", "application/json")
	b, _ := json.Marshal(errorJson{Error: err.Error()})
	_, _ = writer.Write(b)
}

func (n kubeFilter) getOwnedNamespacesForUser(username string) (res NamespaceList, err error) {
	tl := &capsulev1alpha1.TenantList{}
	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", "User", username),
	}
	if err := n.client.List(context.Background(), tl, f); err != nil {
		return nil, fmt.Errorf("cannot retrieve Tenant list: %s", err.Error())
	}

	for _, t := range tl.Items {
		res = append(res, t.GetName())
	}
	return
}

func (n kubeFilter) getLabelSelectorForUser(username string) (labels.Selector, error) {
	capsuleLabel, err := capsulev1alpha1.GetTypeLabel(&capsulev1alpha1.Tenant{})
	if err != nil {
		return nil, fmt.Errorf("cannot get Capsule Tenant label: %s", err.Error())
	}

	ownedNamespaces, err := n.getOwnedNamespacesForUser(username)
	if err != nil {
		return nil, fmt.Errorf("cannot get Namespaces slice owned by Tenant Owner: %s", err.Error())
	}

	var req *labels.Requirement

	if len(ownedNamespaces) > 0 {
		req, err = labels.NewRequirement(capsuleLabel, selection.In, ownedNamespaces)
	} else {
		req, err = labels.NewRequirement("dontexistsignoreme", selection.Exists, []string{})
	}
	if err != nil {
		return nil, fmt.Errorf("cannot parse Tenant selector: %s", err.Error())
	}

	return labels.NewSelector().Add(*req), nil
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
			ns, _ := n.getOwnedNamespacesForUser(username)
			switch i.Operator() {
			case selection.Exists:
				return fmt.Errorf("cannot return list of all Tenant namespaces")
			case selection.In:
				if ss := i.Values().Delete(ns...); ss.Len() > 0 {
					return fmt.Errorf("cannot list Namespaces for the following Tenant(s): %s", strings.Join(ss.List(), ", "))
				}
			default:
				break
			}
		}
	}
	return nil
}

func (n kubeFilter) decorateRequest(writer http.ResponseWriter, request *http.Request) error {
	r := NewHttpRequest(request, n.usernameClaimField)

	if ok, err := r.IsUserInGroup(n.capsuleUserGroup); err != nil {
		return fmt.Errorf("cannot determinate User group: %s", err.Error())
	} else if !ok {
		// not a Capsule user, let's break
		return nil
	}

	var username string
	var err error
	username, err = r.GetUserName()
	if err != nil {
		return fmt.Errorf("cannot determinate username: %s", err.Error())
	}

	var s labels.Selector
	s, err = n.getLabelSelectorForUser(username)
	if err != nil {
		return fmt.Errorf("cannot create label selector: %s", err)
	}

	q := request.URL.Query()
	if e := q.Get("labelSelector"); len(e) > 0 {
		log.V(4).Info("handling current labelSelector", "selector", e)
		if err := n.validateCapsuleLabel(e, username); err != nil {
			return err
		}
		v := strings.Join([]string{e, s.String()}, ",")
		q.Set("labelSelector", v)
		log.V(4).Info("labelSelector updated", "selector", v)
	} else {
		q.Add("labelSelector", s.String())
		log.V(4).Info("labelSelector added", "selector", s.String())
	}
	log.V(4).Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	log.V(4).Info("Updating the token", "token", n.bearerToken)
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", n.bearerToken))

	log.Info("proxying to API Server", "url", request.URL.String())
	return nil
}
