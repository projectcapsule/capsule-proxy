package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/clastix/capsule-ns-filter/webserver"
)

var (
	scheme = runtime.NewScheme()
	log    = ctrl.Log.WithName("main")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))
}

func main() {
	var err error
	var mgr ctrl.Manager

	listeningPort := flag.Uint("listening-port", 9001, "HTTP port the proxy listens to (default: 9001)")
	k8sControlPlaneUrl := flag.String("k8s-control-plane-url", "https://kubernetes.default.svc", "Kubernetes control plane URL (default: https://kubernetes.default.svc)")
	capsuleUserGroup := flag.String("capsule-user-group", "clastix.capsule.io", "The Capsule User Group eligible to create Namespace for Tenant resources (default: clastix.capsule.io)")
	usernameClaimField := flag.String("oidc-username-claim", "preferred_username", "The OIDC field name used to identify the user (default: preferred_username)")
	bindSsl := flag.Bool("enable-ssl", false, "Enable the bind on HTTPS for secure communication (default: false)")
	certPath := flag.String("ssl-cert-path", "/opt/capsule-ns-filter/tls.crt", "Path to the TLS certificate (default: /opt/capsule-ns-filter/tls.crt)")
	keyPath := flag.String("ssl-key-path", "/opt/capsule-ns-filter/tls.key", "Path to the TLS certificate key (default: /opt/capsule-ns-filter/tls.key)")

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	log.Info("---")
	log.Info(fmt.Sprintf("Manager listening on port %d", *listeningPort))
	log.Info(fmt.Sprintf("Listening on HTTPS: %t", *bindSsl))
	log.Info(fmt.Sprintf("Connecting to the Kubernete API Server listening on %s", *k8sControlPlaneUrl))
	log.Info(fmt.Sprintf("The selected Capsule User Group is %s", *capsuleUserGroup))
	log.Info(fmt.Sprintf("The OIDC username selected is %s", *usernameClaimField))
	log.Info("---")

	log.Info("Creating the manager")
	mgr, err = ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		log.Error(err, "cannot create new Manager")
		os.Exit(1)
	}

	log.Info("Creating the Field Indexer")
	ow := tenant.OwnerReference{}
	err = mgr.GetFieldIndexer().IndexField(context.Background(), ow.Object(), ow.Field(), ow.Func())
	if err != nil {
		log.Error(err, "cannot create new Field Indexer")
		os.Exit(1)
	}

	var r manager.Runnable
	log.Info("Creating the NamespaceFilter runner")

	var listenerOpts webserver.ListenerOptions
	listenerOpts, err = webserver.NewKubeOptions(*k8sControlPlaneUrl, *capsuleUserGroup, *usernameClaimField, ctrl.GetConfigOrDie())
	if err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var serverOpts webserver.ServerOptions
	serverOpts, err = webserver.NewServerOptions(*bindSsl, *listeningPort, *certPath, *keyPath)
	if err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	r, err = webserver.NewKubeFilter(listenerOpts, serverOpts)
	if err != nil {
		log.Error(err, "cannot create NamespaceFilter runner")
		os.Exit(1)
	}

	log.Info("Adding the NamespaceFilter runner to the Manager")
	err = mgr.Add(r)
	if err != nil {
		log.Error(err, "cannot add NameSpaceFilter as Runnable")
		os.Exit(1)
	}

	log.Info("Starting the Manager")
	err = mgr.Start(ctrl.SetupSignalHandler())
	if err != nil {
		log.Error(err, "cannot start the Manager")
		os.Exit(1)
	}
}
