// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	capsulev1beta1 "github.com/projectcapsule/capsule/api/v1beta1"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleindexer "github.com/projectcapsule/capsule/pkg/runtime/indexers"
	"github.com/projectcapsule/capsule/pkg/runtime/indexers/tenant"
	flag "github.com/spf13/pflag"
	"github.com/thediveo/enumflag"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	capsuleproxyv1beta1 "github.com/projectcapsule/capsule-proxy/api/v1beta1"
	"github.com/projectcapsule/capsule-proxy/internal/controllers"
	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/indexer"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	"github.com/projectcapsule/capsule-proxy/internal/webserver"
)

// WebhookType defines the available webhook names.
type WebhookType enumflag.Flag

// setupHealthProbes registers liveness and readiness probes on the manager.
func setupHealthProbes(mgr ctrl.Manager, filter webserver.Filter) error {
	if err := mgr.AddHealthzCheck("healthz", filter.LivenessProbe); err != nil {
		return fmt.Errorf("cannot create healthcheck probe: %w", err)
	}

	if err := mgr.AddReadyzCheck("ready", filter.ReadinessProbe); err != nil {
		return fmt.Errorf("cannot create readiness probe: %w", err)
	}

	return nil
}

// setupObservedGenerationControllers registers the status controllers that maintain
// observedGeneration on GlobalProxySettings and ProxySetting resources.
func setupObservedGenerationControllers(mgr ctrl.Manager) error {
	if err := (&controllers.GlobalProxySettingsReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("cannot start GlobalProxySettings controller: %w", err)
	}

	if err := (&controllers.ProxySettingReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("cannot start ProxySetting controller: %w", err)
	}

	return nil
}

const (
	WebhookWatchdog WebhookType = iota
	WebhookLabler
)

func main() {
	scheme := runtime.NewScheme()
	log := ctrl.Log.WithName("main")

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta1.AddToScheme(scheme))
	utilruntime.Must(capsulev1beta2.AddToScheme(scheme))
	utilruntime.Must(capsuleproxyv1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	var (
		err       error
		mgr       ctrl.Manager
		namespace string
	)

	c := &cliOptions{}
	c.bindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&c.logOptions))

	ctrl.SetLogger(logger)

	for feat := range c.gates.GetAll() {
		log.Info("feature gate status", "name", feat, "enabled", c.gates.Enabled(feat))
	}

	if namespace = os.Getenv("NAMESPACE"); len(namespace) == 0 {
		log.Error(fmt.Errorf("unable to determinate the Namespace Proxy is running on"), "unable to start manager")
		os.Exit(1)
	}

	log.Info("---")
	log.Info(fmt.Sprintf("Manager listening on port %d", c.listeningPort))
	log.Info(fmt.Sprintf("Listening on HTTPS: %t", c.bindSsl))

	if err = c.validateTLS(); err != nil {
		log.Info(err.Error())
		os.Exit(1)
	}

	log.Info(fmt.Sprintf("The ignored User Groups are %v", c.ignoredUserGroups))
	log.Info(fmt.Sprintf("The ignored Usernames are %v", c.ignoredUsernames))
	log.Info(fmt.Sprintf("The OIDC username selected is %s", c.usernameClaimField))

	if c.impersonationGroupsRegexp != "" {
		log.Info(fmt.Sprintf("The Group impersonation Regexp %s", c.impersonationGroupsRegexp))
	}

	if len(c.ignoreImpersonationGroups) > 0 {
		log.Info(fmt.Sprintf("The Groups dropped for impersonation %s", c.ignoreImpersonationGroups))
	}

	if c.gates.Enabled(features.SkipImpersonationReview) {
		log.Info("SECURITY IMPLICATION: Skipping Impersonation reviews are enabled!")
	}

	log.Info("---")
	log.Info("Creating the manager")

	config := ctrl.GetConfigOrDie()
	c.configureClient(config)
	ctrlConfig := c.managerOptions(namespace, scheme)

	mgr, err = ctrl.NewManager(config, ctrlConfig)
	if err != nil {
		log.Error(err, "cannot create new Manager")
		os.Exit(1)
	}

	var rbReflector *controllers.RoleBindingReflector

	if c.reflectorEnabled() {
		log.Info("Creating the Rolebindings reflector")

		if rbReflector, err = controllers.NewRoleBindingReflector(context.Background(), mgr.GetCache()); err != nil {
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
		&indexer.TenantOwnerReference{},
		&indexer.ProxySetting{},
		&indexer.GlobalProxySetting{},
	}

	for _, fieldIndex := range indexers {
		if err = mgr.GetFieldIndexer().IndexField(ctx, fieldIndex.Object(), fieldIndex.Field(), fieldIndex.Func()); err != nil {
			log.Error(err, "cannot create new Field Indexer")
			os.Exit(1)
		}
	}

	log.Info("Creating the NamespaceFilter runner")

	var listenerOpts options.ListenerOpts

	if listenerOpts, err = c.listenerOptions(config); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	var serverOpts options.ServerOptions

	if serverOpts, err = c.serverOptions(config); err != nil {
		log.Error(err, "cannot create Kubernetes options")
		os.Exit(1)
	}

	clientOverride := c.reader(mgr)

	r, err := webserver.NewKubeFilter(
		listenerOpts,
		serverOpts,
		c.gates,
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

	if err = c.capsuleConfigurationController(mgr.GetClient()).SetupWithManager(ctx, mgr); err != nil {
		log.Error(err, "cannot start CapsuleConfiguration controller for User Group list retrieval")
		os.Exit(1)
	}

	if err := setupObservedGenerationControllers(mgr); err != nil {
		log.Error(err, "unable to set up observed generation controllers")
		os.Exit(1)
	}

	if err = setupHealthProbes(mgr, r); err != nil {
		log.Error(err, "cannot set up health probes")
		os.Exit(1)
	}

	log.Info("Starting the Manager")

	if err = mgr.Start(ctx); err != nil {
		log.Error(err, "cannot start the Manager")
		os.Exit(1)
	}
}
