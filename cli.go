// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	goflag "flag"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	"github.com/thediveo/enumflag"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	"github.com/projectcapsule/capsule-proxy/internal/request"
)

// cliOptions holds the flags shared by main and the CLI regression tests.
// Keep registration and runtime wiring together so tests exercise the executable's options.
type cliOptions struct {
	certPath, keyPath, usernameClaimField, capsuleConfigurationName                  string
	impersonationGroupsRegexp, metricsAddr, xfccHeaderName                           string
	ignoredUserGroups, ignoredUsernames, ignoreImpersonationGroups                   []string
	allowedPaths, publicPaths, trustedProxyCIDRStrings                               []string
	listeningPort                                                                    uint
	bindSsl, disableCaching, enablePprof, enableLeaderElection, roleBindingReflector bool
	rolebindingsResyncPeriod                                                         time.Duration
	clientConnectionQPS                                                              float32
	clientConnectionBurst                                                            int32
	webhookPort                                                                      int
	hooks                                                                            []WebhookType
	authTypes                                                                        []request.AuthType
	gates                                                                            featuregate.MutableFeatureGate
	logOptions                                                                       zap.Options
}

// bindFlags accepts a fresh FlagSet so repeated parses never share flag state.
func (c *cliOptions) bindFlags(flag *pflag.FlagSet) {
	c.gates = featuregate.NewFeatureGate()

	utilruntime.Must(c.gates.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		features.ProxyAllNamespaced: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
		features.SkipImpersonationReview: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
		features.ProxyClusterScoped: {
			Default:       false,
			LockToDefault: false,
			PreRelease:    featuregate.Alpha,
		},
	}))

	c.authTypes = []request.AuthType{
		request.TLSCertificate,
		request.BearerToken,
	}

	authTypesMap := map[request.AuthType][]string{
		request.BearerToken:          {request.BearerToken.String()},
		request.TLSCertificate:       {request.TLSCertificate.String()},
		request.XForwardedClientCert: {request.XForwardedClientCert.String()},
	}

	flag.IntVar(
		&c.webhookPort,
		"webhook-port",
		9443,
		"The port the webhook server binds to.",
	)
	flag.BoolVar(
		&c.enableLeaderElection,
		"enable-leader-election",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	flag.StringVar(
		&c.metricsAddr,
		"metrics-addr",
		":8080",
		"The address the metric endpoint binds to.",
	)
	flag.StringSliceVar(
		&c.allowedPaths,
		"allowed-paths",
		[]string{
			"/api", "/apis", "/version",
		},
		"URL paths which are not inspected by capsule-proxy (still require valid authentication)",
	)
	flag.StringSliceVar(
		&c.publicPaths,
		"public-paths",
		nil,
		"URL paths passed directly to the upstream server without capsule-proxy authentication (trusted-proxy-cidrs still apply)",
	)
	flag.StringSliceVar(
		&c.trustedProxyCIDRStrings,
		"trusted-proxy-cidrs",
		nil,
		"CIDR ranges of trusted proxies allowed to send forwarded client certificate headers",
	)
	flag.StringVar(
		&c.xfccHeaderName,
		"xfcc-header-name",
		"X-Forwarded-Client-Cert",
		"Name of the header inspected for forwarded client certificates",
	)
	flag.StringVar(
		&c.capsuleConfigurationName,
		"capsule-configuration-name",
		"default",
		"Name of the CapsuleConfiguration used to retrieve the Capsule user groups names",
	)
	flag.StringSliceVar(
		&c.ignoredUserGroups,
		"ignored-user-group",
		[]string{},
		"Names of the groups which requests must be ignored and proxy-passed to the upstream server",
	)
	flag.StringSliceVar(
		&c.ignoredUsernames,
		"ignored-username",
		[]string{},
		"Usernames whose requests must be ignored and proxy-passed to the upstream server",
	)
	flag.StringSliceVar(
		&c.ignoreImpersonationGroups,
		"ignored-impersonation-group",
		[]string{},
		"Names of the groups which are not used for impersonation (considered after impersonation-group-regexp)",
	)
	flag.StringVar(
		&c.impersonationGroupsRegexp,
		"impersonation-group-regexp",
		"",
		"Regular expression to match the groups which are considered for impersonation",
	)
	flag.UintVar(
		&c.listeningPort,
		"listening-port",
		9001,
		"HTTP port the proxy listens to (default: 9001)",
	)
	flag.StringVar(
		&c.usernameClaimField,
		"oidc-username-claim",
		"preferred_username",
		"The OIDC field name used to identify the user (default: preferred_username)",
	)
	flag.BoolVar(
		&c.roleBindingReflector,
		"enable-reflector",
		false,
		"Enable reflection for RoleBindings labelled reflection.proxy.projectcapsule.dev/enabled=true",
	)
	flag.BoolVar(
		&c.enablePprof,
		"enable-pprof",
		false,
		"Enables Pprof endpoint for profiling (not recommend in production)",
	)
	flag.BoolVar(
		&c.bindSsl,
		"enable-ssl",
		true,
		"Enable the bind on HTTPS for secure communication (default: true)",
	)
	flag.StringVar(
		&c.certPath,
		"ssl-cert-path",
		"",
		"Path to the TLS certificate (default: /opt/capsule-proxy/tls.crt)",
	)
	flag.StringVar(
		&c.keyPath,
		"ssl-key-path",
		"",
		"Path to the TLS certificate key (default: /opt/capsule-proxy/tls.key)",
	)
	flag.DurationVar(
		&c.rolebindingsResyncPeriod,
		"rolebindings-resync-period",
		10*time.Hour,
		"Resync period for the Role and RoleBinding reflector",
	)
	flag.Var(
		enumflag.NewSlice(&c.authTypes, "string", authTypesMap, enumflag.EnumCaseSensitive), "auth-preferred-types",
		`Authentication types to be used for requests. Possible Auth Types: [BearerToken, TLSCertificate, XForwardedClientCert]
First match is used and can be specified multiple times as comma separated values or by using the flag multiple times.`,
	)
	flag.BoolVar(
		&c.disableCaching,
		"disable-caching",
		false,
		"Disable the go-client caching to hit directly the Kubernetes API Server, it disables any local caching as the rolebinding reflector (default: false)",
	)
	flag.Float32Var(
		&c.clientConnectionQPS,
		"client-connection-qps",
		20.0,
		"QPS to use for interacting with kubernetes apiserver.",
	)
	flag.Int32Var(
		&c.clientConnectionBurst,
		"client-connection-burst",
		30,
		"Burst to use for interacting with kubernetes apiserver.",
	)
	c.gates.AddFlag(flag)

	c.logOptions = zap.Options{
		EncoderConfigOptions: append([]zap.EncoderConfigOption{}, func(config *zapcore.EncoderConfig) {
			config.EncodeTime = zapcore.ISO8601TimeEncoder
		}),
	}

	var goFlagSet goflag.FlagSet

	c.logOptions.BindFlags(&goFlagSet)
	flag.AddGoFlagSet(&goFlagSet)
}

func (c *cliOptions) validateTLS() error {
	if !c.bindSsl {
		switch {
		case len(c.certPath) > 0:
			return fmt.Errorf("cannot use a Certificate when TLS/SSL mode is disabled")
		case len(c.keyPath) > 0:
			return fmt.Errorf("cannot use a Certificate key when TLS/SSL mode is disabled")
		}
	}

	return nil
}

func (c *cliOptions) configureClient(config *rest.Config) {
	config.QPS = c.clientConnectionQPS
	config.Burst = int(c.clientConnectionBurst)
}

func (c *cliOptions) managerOptions(namespace string, scheme *runtime.Scheme) ctrl.Options {
	ctrlConfig := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: c.metricsAddr,
		},
		HealthProbeBindAddress:  ":8081",
		LeaderElection:          false,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        "42dadw1.proxy.projectcapsule.dev",
	}

	if len(c.hooks) > 0 {
		ctrlConfig.WebhookServer = ctrlwebhook.NewServer(ctrlwebhook.Options{
			Port: c.webhookPort,
		})
	}

	// Conditional config
	if c.enablePprof {
		ctrlConfig.PprofBindAddress = ":8082"
	}

	if c.reflectorEnabled() {
		ctrlConfig.Cache.SyncPeriod = &c.rolebindingsResyncPeriod
	}

	return ctrlConfig
}

func (c *cliOptions) reflectorEnabled() bool {
	return !c.disableCaching && c.roleBindingReflector
}

func (c *cliOptions) reader(mgr ctrl.Manager) client.Reader {
	if c.disableCaching {
		return mgr.GetAPIReader()
	}

	return mgr.GetClient()
}

func (c *cliOptions) capsuleConfigurationController(cl client.Client) *controllers.CapsuleConfiguration {
	return &controllers.CapsuleConfiguration{
		Client:                   cl,
		CapsuleConfigurationName: c.capsuleConfigurationName,
	}
}

func (c *cliOptions) listenerOptions(config *rest.Config) (options.ListenerOpts, error) {
	return options.NewKube(
		c.authTypes, c.ignoredUserGroups, c.ignoredUsernames, c.usernameClaimField,
		config, c.ignoreImpersonationGroups, c.impersonationGroupsRegexp,
		c.gates.Enabled(features.SkipImpersonationReview), c.trustedProxyCIDRStrings,
		c.xfccHeaderName, c.allowedPaths, c.publicPaths,
	)
}

func (c *cliOptions) serverOptions(config *rest.Config) (options.ServerOptions, error) {
	if err := c.validateTLS(); err != nil {
		return nil, err
	}

	return options.NewServer(c.bindSsl, c.listeningPort, c.certPath, c.keyPath, config)
}
