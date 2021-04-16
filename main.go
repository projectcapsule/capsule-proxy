package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/clastix/capsule-proxy/internal/options"
	"github.com/clastix/capsule-proxy/internal/webserver"
)

// nolint:funlen
func main() {
	scheme := runtime.NewScheme()
	log := ctrl.Log.WithName("main")

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))

	var err error

	var mgr ctrl.Manager

	listeningPort := flag.Uint("listening-port", 9001, "HTTP port the proxy listens to (default: 9001)")
	capsuleUserGroup := flag.String("capsule-user-group", "capsule.clastix.io", "The Capsule User Group eligible to create Namespace for Tenant resources (default: capsule.clastix.io)")
	usernameClaimField := flag.String("oidc-username-claim", "preferred_username", "The OIDC field name used to identify the user (default: preferred_username)")
	bindSsl := flag.Bool("enable-ssl", false, "Enable the bind on HTTPS for secure communication (default: false)")
	certPath := flag.String("ssl-cert-path", "/opt/capsule-proxy/tls.crt", "Path to the TLS certificate (default: /opt/capsule-proxy/tls.crt)")
	keyPath := flag.String("ssl-key-path", "/opt/capsule-proxy/tls.key", "Path to the TLS certificate key (default: /opt/capsule-proxy/tls.key)")

	opts := zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	log.Info("---")
	log.Info(fmt.Sprintf("Manager listening on port %d", *listeningPort))
	log.Info(fmt.Sprintf("Listening on HTTPS: %t", *bindSsl))

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

	if err = mgr.GetFieldIndexer().IndexField(context.Background(), ow.Object(), ow.Field(), ow.Func()); err != nil {
		log.Error(err, "cannot create new Field Indexer")
		os.Exit(1)
	}

	var r webserver.Filter

	log.Info("Creating the NamespaceFilter runner")

	var listenerOpts options.ListenerOpts

	if listenerOpts, err = options.NewKube(*capsuleUserGroup, *usernameClaimField, ctrl.GetConfigOrDie()); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var serverOpts options.ServerOptions

	if serverOpts, err = options.NewServer(*bindSsl, *listeningPort, *certPath, *keyPath, ctrl.GetConfigOrDie()); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	r, err = webserver.NewKubeFilter(listenerOpts, serverOpts)
	if err != nil {
		log.Error(err, "cannot create NamespaceFilter runner")
		os.Exit(1)
	}

	log.Info("Adding the NamespaceFilter runner to the Manager")

	if err = mgr.Add(r); err != nil {
		log.Error(err, "cannot add NameSpaceFilter as Runnable")
		os.Exit(1)
	}

	if err = mgr.AddHealthzCheck("healthz", r.LivenessProbe); err != nil {
		log.Error(err, "cannot create healthcheck probe")
		os.Exit(1)
	}

	if err = mgr.AddReadyzCheck("ready", r.ReadinessProbe); err != nil {
		log.Error(err, "cannot create readiness probe")
		os.Exit(1)
	}

	log.Info("Starting the Manager")

	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "cannot start the Manager")
		os.Exit(1)
	}
}
