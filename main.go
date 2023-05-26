// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package main

import (
	goflag "flag"
	"fmt"
	"os"
	"time"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"
	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	capsuleindexer "github.com/clastix/capsule/pkg/indexer"
	"github.com/clastix/capsule/pkg/indexer/tenant"
	flag "github.com/spf13/pflag"
	"github.com/thediveo/enumflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsuleproxyv1beta1 "github.com/clastix/capsule-proxy/api/v1beta1"
	"github.com/clastix/capsule-proxy/internal/controllers"
	"github.com/clastix/capsule-proxy/internal/indexer"
	"github.com/clastix/capsule-proxy/internal/options"
	"github.com/clastix/capsule-proxy/internal/request"
	"github.com/clastix/capsule-proxy/internal/webserver"
)

//nolint:funlen,cyclop
func main() {
	scheme := runtime.NewScheme()
	log := ctrl.Log.WithName("main")

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1alpha1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	utilruntime.Must(capsuleproxyv1beta1.AddToScheme(scheme))

	var err error

	var mgr ctrl.Manager

	var certPath, keyPath, usernameClaimField, capsuleConfigurationName string

	var capsuleUserGroups, ignoredUserGroups []string

	var listeningPort uint

	var bindSsl, disableCaching bool

	var rolebindingsResyncPeriod time.Duration

	var clientConnectionQPS float32

	var clientConnectionBurst int32

	authTypes := []request.AuthType{
		request.TLSCertificate,
		request.BearerToken,
	}

	authTypesMap := map[request.AuthType][]string{
		request.BearerToken:    {request.BearerToken.String()},
		request.TLSCertificate: {request.TLSCertificate.String()},
	}

	flag.StringVar(&capsuleConfigurationName, "capsule-configuration-name", "default", "Name of the CapsuleConfiguration used to retrieve the Capsule user groups names")
	flag.StringSliceVar(&capsuleUserGroups, "capsule-user-group", []string{}, "Names of the groups for capsule users (deprecated: use capsule-configuration-name)")
	flag.StringSliceVar(&ignoredUserGroups, "ignored-user-group", []string{}, "Names of the groups which requests must be ignored and proxy-passed to the upstream server")
	flag.UintVar(&listeningPort, "listening-port", 9001, "HTTP port the proxy listens to (default: 9001)")
	flag.StringVar(&usernameClaimField, "oidc-username-claim", "preferred_username", "The OIDC field name used to identify the user (default: preferred_username)")
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

	opts := zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}

	var goFlagSet goflag.FlagSet

	opts.BindFlags(&goFlagSet)
	flag.CommandLine.AddGoFlagSet(&goFlagSet)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
	log.Info("---")
	log.Info("Creating the manager")

	config := ctrl.GetConfigOrDie()
	config.QPS = clientConnectionQPS
	config.Burst = int(clientConnectionBurst)

	mgr, err = ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
	})
	if err != nil {
		log.Error(err, "cannot create new Manager")
		os.Exit(1)
	}

	var rbReflector *controllers.RoleBindingReflector

	if !disableCaching {
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
		log.Info("Cache is disabled, cannot create Rolebindings reflector")
	}

	ctx := ctrl.SetupSignalHandler()

	log.Info("Creating the Field Indexer")

	indexers := []capsuleindexer.CustomIndexer{
		&tenant.NamespacesReference{Obj: &capsulev1beta2.Tenant{}},
		&tenant.OwnerReference{},
		&indexer.ProxySetting{},
	}

	for _, fieldIndex := range indexers {
		if err = mgr.GetFieldIndexer().IndexField(ctx, fieldIndex.Object(), fieldIndex.Field(), fieldIndex.Func()); err != nil {
			log.Error(err, "cannot create new Field Indexer")
			os.Exit(1)
		}
	}

	var r webserver.Filter

	log.Info("Creating the NamespaceFilter runner")

	var listenerOpts options.ListenerOpts

	if listenerOpts, err = options.NewKube(authTypes, ignoredUserGroups, usernameClaimField, config); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var serverOpts options.ServerOptions

	if serverOpts, err = options.NewServer(bindSsl, listeningPort, certPath, keyPath, config); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var clientOverride client.Reader

	if !disableCaching {
		clientOverride = mgr.GetAPIReader()
	}

	r, err = webserver.NewKubeFilter(listenerOpts, serverOpts, rbReflector, clientOverride)
	if err != nil {
		log.Error(err, "cannot create NamespaceFilter runner")
		os.Exit(1)
	}

	log.Info("Adding the NamespaceFilter runner to the Manager")

	if err = (&controllers.CapsuleConfiguration{
		CapsuleConfigurationName:    capsuleConfigurationName,
		DeprecatedCapsuleUserGroups: capsuleUserGroups,
	}).SetupWithManager(ctx, mgr); err != nil {
		log.Error(err, "cannot start CapsuleConfiguration controller for User Group list retrieval")
		os.Exit(1)
	}

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

	if err = mgr.Start(ctx); err != nil {
		log.Error(err, "cannot start the Manager")
		os.Exit(1)
	}
}
