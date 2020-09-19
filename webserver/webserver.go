package webserver

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	log = ctrl.Log.WithName("namespace_filter")
)

func NewKubeFilter(listeningPort uint, controlPlaneUrl, capsuleUserGroup, usernameClaimField string, config *rest.Config) (*kubeFilter, error) {
	u, err := url.Parse(controlPlaneUrl)
	if err != nil {
		log.Error(err, "cannot parse Kubernetes Control Plane URL")
		return nil, err
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(u)
	reverseProxy.FlushInterval = time.Millisecond * 100
	reverseProxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, e error) {
			return (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: func() *tls.Config {
			cp, _ := cert.NewPoolFromBytes(config.CAData)
			return &tls.Config{
				InsecureSkipVerify: true,
				Certificates: append([]tls.Certificate{}, tls.Certificate{
					Certificate: append([][]byte{}, config.CertData),
					PrivateKey:  append([][]byte{}, config.KeyData),
				}),
				RootCAs:    cp,
				NextProtos: config.NextProtos,
				ServerName: config.ServerName,
			}
		}(),
	}

	return &kubeFilter{
		capsuleUserGroup:   capsuleUserGroup,
		reverseProxy:       reverseProxy,
		listeningPort:      listeningPort,
		bearerToken:        config.BearerToken,
		usernameClaimField: usernameClaimField,
	}, nil
}

type kubeFilter struct {
	capsuleUserGroup   string
	reverseProxy       *httputil.ReverseProxy
	client             client.Client
	listeningPort      uint
	bearerToken        string
	usernameClaimField string
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

func (n kubeFilter) Start(stop <-chan struct{}) error {
	http.HandleFunc("/api/v1/namespaces", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == "GET" || n.isWatchEndpoint(request) {
			log.Info("decorating /api/v1/namespaces request")
			if err := n.decorateRequest(writer, request); err != nil {
				n.handleError(err, writer)
				return
			}
		}
		n.reverseProxyFunc(writer, request)
	})
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		n.reverseProxyFunc(writer, request)
	})
	http.HandleFunc("/_healthz", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		_, _ = writer.Write([]byte("ok"))
	})

	go func() {
		if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", n.listeningPort), nil); err != nil {
			panic(err)
		}
	}()

	<-stop

	return nil
}

func (n kubeFilter) reverseProxyFunc(writer http.ResponseWriter, request *http.Request) {
	log.Info("handling " + request.URL.String())
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
		v := e + "," + s.String()
		q.Add("labelSelector", v)
		log.Info("labelSelector updated", "selector", v)
	} else {
		q.Add("labelSelector", s.String())
		log.Info("labelSelector updated", "selector", s.String())
	}
	log.Info("updating RawQuery", "query", q.Encode())
	request.URL.RawQuery = q.Encode()

	log.Info("Updating the token", "token", n.bearerToken)
	request.Header.Set("Authorization", "Bearer "+n.bearerToken)

	log.Info("proxying to API Server", "url", request.URL.String())
	return nil
}
