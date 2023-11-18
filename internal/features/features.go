// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package features

// Credits: https://github.com/fluxcd/pkg/main/runtime/features/features.go

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
)

const (
	// ProxyAllNamespaced allows to proxy all the Namespaced objects
	// for all tenant users
	//
	// When enabled, it will discover apis and ensure labels are set
	// for resources in all tenant namespaces resulting in increased memory
	// usage and cluster-wide RBAC permissions (list and watch).
	ProxyAllNamespaced = "ProxyAllNamespaced"
)

const (
	flagFeatureGates = "feature-gates"
)

var loaded bool

var featureGates = map[string]bool{
	// ProxyAllNamespaced
	ProxyAllNamespaced: false,
}

type FeatureGates struct {
	log         *logr.Logger
	cliFeatures map[string]bool
}

// WithLogger sets the logger to be used when loading supported features.
func (o *FeatureGates) WithLogger(l logr.Logger) *FeatureGates {
	o.log = &l
	return o
}

func ALLFeatureGates() map[string]bool {
	return featureGates
}

// SupportedFeatures sets the supported features and their default values.
func (o *FeatureGates) SupportedFeatures() error {
	loaded = true

	for k, v := range o.cliFeatures {
		if _, ok := featureGates[k]; ok {
			featureGates[k] = v
		} else {
			return fmt.Errorf("feature-gate '%s' not supported", k)
		}
		if o.log != nil {
			o.log.Info("loading feature gate", k, v)
		}
	}
	return nil
}

// Enabled verifies whether the feature is enabled or not.
func Enabled(feature string) (bool, error) {
	if !loaded {
		return false, fmt.Errorf("supported features not set")
	}
	if enabled, ok := featureGates[feature]; ok {
		return enabled, nil
	}
	return false, fmt.Errorf("feature-gate '%s' not supported", feature)
}

// BindFlags will parse the given pflag.FlagSet and load feature gates accordingly.
func (o *FeatureGates) BindFlags(fs *pflag.FlagSet) {
	fs.Var(cliflag.NewMapStringBool(&o.cliFeatures), flagFeatureGates,
		"A comma separated list of key=value pairs defining the state of experimental features.")
}

// BindFlags will parse the given pflag.FlagSet and load feature gates accordingly.
func Disable(feature string) {
	if _, ok := featureGates[feature]; ok {
		featureGates[feature] = false
	}
}
