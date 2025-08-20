// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	goflag "flag"
	"fmt"
	"os"
	"time"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleindexer "github.com/projectcapsule/capsule/pkg/indexer"
	"github.com/projectcapsule/capsule/pkg/indexer/tenant"
	flag "github.com/spf13/pflag"
	"github.com/thediveo/enumflag"
	"go.uber.org/zap/zapcore"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/controllers/watchdog"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/webhooks"
	"github.com/projectcapsule/capsule-proxy/internal/webserver"
)

// WebhookType defines the available webhook names.
type WebhookType enumflag.Flag

const (
	WebhookWatchdog WebhookType = iota
	WebhookLabler
)

//nolint:funlen,gocyclo,cyclop,maintidx
func main() {
	scheme := runtime.NewScheme()
	log := ctrl.Log.WithName("main")

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	utilruntime.Must(capsuleproxyv1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	var (
		err                                                                                                                error
		mgr                                                                                                                ctrl.Manager
		namespace, certPath, keyPath, usernameClaimField, capsuleConfigurationName, impersonationGroupsRegexp, metricsAddr string
		capsuleUserGroups, ignoredUserGroups, ignoreImpersonationGroups                                                    []string
		listeningPort                                                                                                      uint
		bindSsl, disableCaching, enablePprof, enableLeaderElection, roleBindingReflector                                   bool
		rolebindingsResyncPeriod                                                                                           time.Duration
		clientConnectionQPS                                                                                                float32
		clientConnectionBurst                                                                                              int32
		webhookPort                                                                                                        int
		hooks                                                                                                              []WebhookType
	)

	gates := featuregate.NewFeatureGate()

	utilruntime.Must(gates.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		features.ProxyAllNamespaced: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
		features.ProxyClusterScoped: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
		features.SkipImpersonationReview: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
	}))

	authTypes := []request.AuthType{
		request.TLSCertificate,
		request.BearerToken,
	}

	authTypesMap := map[request.AuthType][]string{
		request.BearerToken:    {request.BearerToken.String()},
		request.TLSCertificate: {request.TLSCertificate.String()},
	}

	WebhookTypeStrings := map[WebhookType][]string{
		WebhookWatchdog: {"Watchdog"},
		WebhookLabler:   {"Labler"},
	}

	flag.IntVar(&webhookPort, "webhook-port", 9443, "The port the webhook server binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&capsuleConfigurationName, "capsule-configuration-name", "default", "Name of the CapsuleConfiguration used to retrieve the Capsule user groups names")
	flag.StringSliceVar(&capsuleUserGroups, "capsule-user-group", []string{}, "Names of the groups for capsule users (deprecated: use capsule-configuration-name)")
	flag.StringSliceVar(&ignoredUserGroups, "ignored-user-group", []string{}, "Names of the groups which requests must be ignored and proxy-passed to the upstream server")
	flag.StringSliceVar(&ignoreImpersonationGroups, "ignored-impersonation-group", []string{}, "Names of the groups which are not used for impersonation (considered after impersonation-group-regexp)")
	flag.StringVar(&impersonationGroupsRegexp, "impersonation-group-regexp", "", "Regular expression to match the groups which are considered for impersonation")
	flag.UintVar(&listeningPort, "listening-port", 9001, "HTTP port the proxy listens to (default: 9001)")
	flag.StringVar(&usernameClaimField, "oidc-username-claim", "preferred_username", "The OIDC field name used to identify the user (default: preferred_username)")
	flag.BoolVar(&roleBindingReflector, "enable-reflector", false, "Enable rolebinding reflector. The reflector allows to list the namespaces, where a rolebinding mentions a user")
	flag.BoolVar(&enablePprof, "enable-pprof", false, "Enables Pprof endpoint for profiling (not recommend in production)")
	flag.BoolVar(&bindSsl, "enable-ssl", true, "Enable the bind on HTTPS for secure communication (default: true)")
	flag.StringVar(&certPath, "ssl-cert-path", "", "Path to the TLS certificate (default: /opt/capsule-proxy/tls.crt)")
	flag.StringVar(&keyPath, "ssl-key-path", "", "Path to the TLS certificate key (default: /opt/capsule-proxy/tls.key)")
	flag.DurationVar(&rolebindingsResyncPeriod, "rolebindings-resync-period", 10*time.Hour, "Resync period for rolebindings reflector")
	flag.Var(enumflag.NewSlice(&authTypes, "string", authTypesMap, enumflag.EnumCaseSensitive), "auth-preferred-types",
		`Authentication types to be used for requests. Possible Auth Types: [BearerToken, TLSCertificate]
First match is used and can be specified multiple times as comma separated values or by using the flag multiple times.`)
	flag.BoolVar(&disableCaching, "disable-caching", false, "Disable the go-client caching to hit directly the Kubernetes API Server, it disables any local caching as the rolebinding reflector (default: false)")
	flag.Float32Var(&clientConnectionQPS, "client-connection-qps", 20.0, "QPS to use for interacting with kubernetes apiserver.")
	flag.Int32Var(&clientConnectionBurst, "client-connection-burst", 30, "Burst to use for interacting with kubernetes apiserver.")
	flag.Var(
		enumflag.NewSlice(&hooks, "string", WebhookTypeStrings, enumflag.EnumCaseInsensitive),
		"webhooks",
		"Comma-separated list of webhooks to enable. Available options: Watchdog, Labler",
	)
	gates.AddFlag(flag.CommandLine)

	opts := zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}

	var goFlagSet goflag.FlagSet

	opts.BindFlags(&goFlagSet)
	flag.CommandLine.AddGoFlagSet(&goFlagSet)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))

	ctrl.SetLogger(logger)

	for feat := range gates.GetAll() {
		log.Info("feature gate status", "name", feat, "enabled", gates.Enabled(feat))
	}

	if namespace = os.Getenv("NAMESPACE"); len(namespace) == 0 {
		log.Error(fmt.Errorf("unable to determinate the Namespace Proxy is running on"), "unable to start manager")
		os.Exit(1)
	}

	log.Info("---")
	log.Info(fmt.Sprintf("Manager listening on port %d", listeningPort))
	log.Info(fmt.Sprintf("Listening on HTTPS: %t", bindSsl))

	if !bindSsl {
		switch {
		case len(certPath) > 0:
			log.Info("cannot use a Certificate when TLS/SSL mode is disabled")
			os.Exit(1)
		case len(keyPath) > 0:
			log.Info("cannot use a Certificate key when TLS/SSL mode is disabled")
			os.Exit(1)
		}
	}

	if len(capsuleUserGroups) > 0 {
		log.Info(
			"the CLI flags --capsule-user-group is deprecated, " +
				"please use the new one --capsule-configuration-name to select the CapsuleConfiguration")
		log.Info(fmt.Sprintf("The selected Capsule User Groups are %v", capsuleUserGroups))
	}

	log.Info(fmt.Sprintf("The ignored User Groups are %v", ignoredUserGroups))
	log.Info(fmt.Sprintf("The OIDC username selected is %s", usernameClaimField))

	if impersonationGroupsRegexp != "" {
		log.Info(fmt.Sprintf("The Group impersonation Regexp %s", impersonationGroupsRegexp))
	}

	if len(ignoreImpersonationGroups) > 0 {
		log.Info(fmt.Sprintf("The Groups dropped for impersonation %s", ignoreImpersonationGroups))
	}

	if gates.Enabled(features.SkipImpersonationReview) {
		log.Info("SECURITY IMPLICATION: Skipping Impersonation reviews are enabled!")
	}

	log.Info("---")
	log.Info("Creating the manager")

	config := ctrl.GetConfigOrDie()
	config.QPS = clientConnectionQPS
	config.Burst = int(clientConnectionBurst)

	// Base Config
	ctrlConfig := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress:  ":8081",
		LeaderElection:          false,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        "42dadw1.proxy.projectcapsule.dev",
	}

	if len(hooks) > 0 {
		ctrlConfig.WebhookServer = ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: webhookPort,
		})
	}

	// Conditional config
	if enablePprof {
		ctrlConfig.PprofBindAddress = ":8082"
	}

	mgr, err = ctrl.NewManager(config, ctrlConfig)
	if err != nil {
		log.Error(err, "cannot create new Manager")
		os.Exit(1)
	}

	var rbReflector *controllers.RoleBindingReflector

	if !disableCaching && roleBindingReflector {
		log.Info("Creating the Rolebindings reflector")

		if rbReflector, err = controllers.NewRoleBindingReflector(config, rolebindingsResyncPeriod); err != nil {
			log.Error(err, "cannot create Rolebindings reflector")
			os.Exit(1)
		}

		log.Info("Adding the Rolebindings reflector to the Manager")

		if err = mgr.Add(rbReflector); err != nil {
			log.Error(err, "cannot add Rolebindings reflector as Runnable")
			os.Exit(1)
		}
	} else {
		log.Info("Rolebinding reflector disabled")
	}

	ctx := ctrl.SetupSignalHandler()

	log.Info("Creating the Field Indexer")

	indexers := []capsuleindexer.CustomIndexer{
		&tenant.NamespacesReference{Obj: &capsulev1beta2.Tenant{}},
		&tenant.OwnerReference{},
		&indexer.ProxySetting{},
	}
	// Optional Indexers
	if gates.Enabled(features.ProxyClusterScoped) {
		indexers = append(indexers, &indexer.GlobalProxySetting{})
	}

	for _, fieldIndex := range indexers {
		if err = mgr.GetFieldIndexer().IndexField(ctx, fieldIndex.Object(), fieldIndex.Field(), fieldIndex.Func()); err != nil {
			log.Error(err, "cannot create new Field Indexer")
			os.Exit(1)
		}
	}

	log.Info("Creating the NamespaceFilter runner")

	var listenerOpts options.ListenerOpts

	if listenerOpts, err = options.NewKube(authTypes, ignoredUserGroups, usernameClaimField, config, ignoreImpersonationGroups, impersonationGroupsRegexp, gates.Enabled(features.SkipImpersonationReview)); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var serverOpts options.ServerOptions

	if serverOpts, err = options.NewServer(bindSsl, listeningPort, certPath, keyPath, config); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var clientOverride client.Reader

	if disableCaching {
		clientOverride = mgr.GetAPIReader()
	} else {
		clientOverride = mgr.GetClient()
	}

	r, err := webserver.NewKubeFilter(
		listenerOpts,
		serverOpts,
		gates,
		rbReflector,
		clientOverride,
		mgr)
	if err != nil {
		log.Error(err, "cannot create NamespaceFilter runner")
		os.Exit(1)
	}

	if err = mgr.Add(r); err != nil {
		log.Error(err, "cannot add NameSpaceFilter as Runnable")
		os.Exit(1)
	}

	if err = (&controllers.CapsuleConfiguration{
		Client:                      mgr.GetClient(),
		CapsuleConfigurationName:    capsuleConfigurationName,
		DeprecatedCapsuleUserGroups: capsuleUserGroups,
	}).SetupWithManager(ctx, mgr); err != nil {
		log.Error(err, "cannot start CapsuleConfiguration controller for User Group list retrieval")
		os.Exit(1)
	}

	if gates.Enabled(features.ProxyAllNamespaced) {
		if err = (&watchdog.CRDWatcher{Client: mgr.GetClient(), LeaderElection: enableLeaderElection}).SetupWithManager(ctx, mgr); err != nil {
			log.Error(err, "cannot start watchdog.CRDWatcher controller for features.ProxyAllNamespaced")
			os.Exit(1)
		}
	}

	// Webhook Reconciler
	if len(hooks) > 0 {
		if containsWebhook(WebhookWatchdog, hooks) {
			mgr.GetWebhookServer().Register("/mutate/watchdog", &admission.Webhook{
				Handler: &webhooks.WatchdogWebhook{
					Decoder: admission.NewDecoder(mgr.GetScheme()),
					Client:  mgr.GetClient(),
					Log:     logger.WithName("Webhooks.Watchdog"),
				},
			})
		}
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

	if err = mgr.Start(ctx); err != nil {
		log.Error(err, "cannot start the Manager")
		os.Exit(1)
	}
}

func containsWebhook(target WebhookType, enabledWebhooks []WebhookType) bool {
	for _, webhook := range enabledWebhooks {
		if webhook == target {
			return true
		}
	}

	return false
}
