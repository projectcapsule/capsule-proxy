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
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	log = ctrl.Log.WithName("namespace_filter")
)

func NewKubeFilter(listeningPort uint, controlPlaneUrl, capsuleUserGroup string, config *rest.Config) (*kubeFilter, error) {
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

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &kubeFilter{
		capsuleUserGroup:   capsuleUserGroup,
		reverseProxy:       reverseProxy,
		listeningPort:      listeningPort,
		namespaceClientSet: cs.CoreV1().Namespaces(),
	}, nil
}

type kubeFilter struct {
	capsuleUserGroup   string
	reverseProxy       *httputil.ReverseProxy
	client             client.Client
	listeningPort      uint
	namespaceClientSet v1.NamespaceInterface
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
		if n.isWatchEndpoint(request) {
			log.Info("handling /api/v1/namespaces for WebSocket")
			n.filterWsNamespace(writer, request)
		}
		if request.Method == "GET" {
			log.Info("handling /api/v1/namespaces")
			n.filterHttpNamespace(writer, request)
			return
		}
		n.reverseProxyFunc(writer, request)
	})
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Info("handling /")
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
	n.reverseProxy.ServeHTTP(writer, request)
}

type errorJson struct {
	Error string `json:"error"`
}

func (n kubeFilter) handleError(err error, msg string, writer http.ResponseWriter) {
	log.Error(err, msg)
	writer.WriteHeader(500)
	b, _ := json.Marshal(errorJson{Error: err.Error()})
	_, _ = writer.Write(b)
}

func (n kubeFilter) getOwnedNamespacesForUser(username string) (res NamespaceList, err error) {
	tl := &capsulev1alpha1.TenantList{}
	f := client.MatchingFields{
		".spec.owner.ownerkind": fmt.Sprintf("%s:%s", "User", username),
	}
	if err := n.client.List(context.TODO(), tl, f); err != nil {
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

func (n kubeFilter) filterHttpNamespace(writer http.ResponseWriter, request *http.Request) {
	r := NewHttpRequest(request)

	if ok, err := r.IsUserInGroup(n.capsuleUserGroup); err != nil {
		n.handleError(err, "cannot determinate User group", writer)
		return
	} else if !ok {
		n.reverseProxyFunc(writer, request)
		return
	}

	var username string
	var err error
	username, err = r.GetUserName()
	if err != nil {
		n.handleError(err, "cannot determinate username", writer)
		return
	}

	var s labels.Selector
	s, err = n.getLabelSelectorForUser(username)
	if err != nil {
		n.handleError(err, "cannot create label selector", writer)
		return
	}

	nl := &corev1.NamespaceList{}
	err = n.client.List(context.TODO(), nl, &client.ListOptions{
		LabelSelector: s,
	})
	if err != nil {
		n.handleError(err, "cannot list Tenant resources", writer)
		return
	}
	var b []byte
	b, err = json.Marshal(nl)
	if err != nil {
		n.handleError(err, "cannot marshal Namespace List resource", writer)
		return
	}
	_, _ = writer.Write(b)
}

func (n kubeFilter) namespacesGet(proxy *httputil.ReverseProxy) func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		if len(request.Header.Get("Upgrade")) > 0 {
			n.filterWsNamespace(writer, request)
			return
		}
		if request.Method == "GET" {
			n.filterHttpNamespace(writer, request)
			return
		}
		n.reverseProxyFunc(writer, request)
	}
}

func (n *kubeFilter) filterWsNamespace(writer http.ResponseWriter, request *http.Request) {
	r := NewHttpRequest(request)

	if ok, err := r.IsUserInGroup(n.capsuleUserGroup); err != nil {
		log.Error(err, "cannot determinate User group")
		panic(err)
	} else if !ok {
		n.reverseProxyFunc(writer, request)
		return
	}

	var username string
	var err error
	username, err = r.GetUserName()
	if err != nil {
		log.Error(err, "cannot determinate User username")
		panic(err)
	}

	u := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	c, err := u.Upgrade(writer, request, nil)
	if err != nil {
		log.Error(err, "cannot upgrade connection")
		panic(err)
	}
	defer func() {
		_ = c.Close()
	}()

	s, _ := n.getLabelSelectorForUser(username)
	watch, err := n.namespaceClientSet.Watch(context.Background(), metav1.ListOptions{
		LabelSelector: s.String(),
		Watch:         true,
	})
	if err != nil {
		log.Error(err, "cannot start watch")
		panic(err)
	}

	for event := range watch.ResultChan() {
		err = c.WriteMessage(websocket.TextMessage, NewMessage(event).Serialize())
		if err != nil {
			log.Error(err, "cannot write websocket message")
			watch.Stop()
		}
	}
}
