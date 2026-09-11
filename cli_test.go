// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/pflag"
	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/projectcapsule/capsule-proxy/internal/features"
	"github.com/projectcapsule/capsule-proxy/internal/options"
	"github.com/projectcapsule/capsule-proxy/internal/request"
	"github.com/projectcapsule/capsule-proxy/internal/webserver/middleware"
)

func parseCLI(t *testing.T, args ...string) *cliOptions {
	t.Helper()
	c, fs := cliFlagSet()
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return c
}

func cliFlagSet() (*cliOptions, *pflag.FlagSet) {
	c := &cliOptions{}
	fs := pflag.NewFlagSet("capsule-proxy", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.bindFlags(fs)
	return c, fs
}

func cliListener(t *testing.T, c *cliOptions) options.ListenerOpts {
	t.Helper()
	o, err := c.listenerOptions(&rest.Config{Host: "https://kubernetes.example"})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// This explicit inventory fails when a flag is added, removed, or its default
// changes. A new flag must also gain a runtime behavior test below.
func TestCLIFlagInventory(t *testing.T) {
	t.Parallel()
	_, fs := cliFlagSet()
	want := map[string]string{
		"allowed-paths":               "[/api,/apis,/version]",
		"auth-preferred-types":        "[TLSCertificate,BearerToken]",
		"capsule-configuration-name":  "default",
		"client-connection-burst":     "30",
		"client-connection-qps":       "20",
		"disable-caching":             "false",
		"enable-leader-election":      "false",
		"enable-pprof":                "false",
		"enable-reflector":            "false",
		"enable-ssl":                  "true",
		"feature-gates":               "",
		"ignored-impersonation-group": "[]",
		"ignored-user-group":          "[]",
		"ignored-username":            "[]",
		"impersonation-group-regexp":  "",
		"listening-port":              "9001",
		"metrics-addr":                ":8080",
		"oidc-username-claim":         "preferred_username",
		"public-paths":                "[]",
		"rolebindings-resync-period":  "10h0m0s",
		"ssl-cert-path":               "",
		"ssl-key-path":                "",
		"trusted-proxy-cidrs":         "[]",
		"webhook-port":                "9443",
		"xfcc-header-name":            "X-Forwarded-Client-Cert",
		"zap-devel":                   "false",
		"zap-encoder":                 "",
		"zap-log-level":               "",
		"zap-stacktrace-level":        "",
		"zap-time-encoding":           "",
	}
	got := map[string]string{}
	fs.VisitAll(func(f *pflag.Flag) { got[f.Name] = f.DefValue })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI defaults changed; add/update behavior coverage:\ngot  %v\nwant %v", got, want)
	}
}

func TestCLIHelp(t *testing.T) {
	t.Parallel()
	_, fs := cliFlagSet()
	var help bytes.Buffer
	fs.SetOutput(&help)
	if err := fs.Parse([]string{"--help"}); !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("--help = %v, want ErrHelp without starting Kubernetes", err)
	}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" && !strings.Contains(help.String(), "--"+f.Name) {
			t.Errorf("help omitted --%s", f.Name)
		}
	})
}

func TestCLIOptionWiring(t *testing.T) {
	t.Parallel()
	tests := []struct {
		flag, value string
		want        any
		read        func(*cliOptions) any
	}{
		{"allowed-paths", "/health,/custom", []string{"/health", "/custom"}, func(c *cliOptions) any { return cliListener(t, c).AllowedPaths() }},
		{"public-paths", "/ready,/live", []string{"/ready", "/live"}, func(c *cliOptions) any { return cliListener(t, c).PublicPaths() }},
		{"ignored-user-group", "platform,ops", []string{"platform", "ops"}, func(c *cliOptions) any { return cliListener(t, c).IgnoredGroupNames() }},
		{"ignored-username", "alice,bob", []string{"alice", "bob"}, func(c *cliOptions) any { return cliListener(t, c).IgnoredUsernames() }},
		{"ignored-impersonation-group", "admin,root", []string{"admin", "root"}, func(c *cliOptions) any { return cliListener(t, c).IgnoredImpersonationsGroups() }},
		{"impersonation-group-regexp", "^tenant:", "^tenant:", func(c *cliOptions) any { return cliListener(t, c).ImpersonationGroupsRegexp().String() }},
		{"oidc-username-claim", "email", "email", func(c *cliOptions) any { return cliListener(t, c).PreferredUsernameClaim() }},
		{"xfcc-header-name", "X-Client-Cert", "X-Client-Cert", func(c *cliOptions) any { return cliListener(t, c).XFCCHeader() }},
		{"trusted-proxy-cidrs", "10.2.3.4/8,2001:db8::/32", []string{"10.0.0.0/8", "2001:db8::/32"}, func(c *cliOptions) any {
			var cidrs []string
			for _, cidr := range cliListener(t, c).TrustedProxyCIDRs() {
				cidrs = append(cidrs, cidr.String())
			}
			return cidrs
		}},
		// The generated AuthType.String() is stale: the accepted XFCC spelling is AuthType(3).
		{"auth-preferred-types", "AuthType(3),BearerToken", []request.AuthType{request.XForwardedClientCert, request.BearerToken}, func(c *cliOptions) any { return cliListener(t, c).AuthTypes() }},
		{"metrics-addr", "127.0.0.1:8180", "127.0.0.1:8180", func(c *cliOptions) any { return c.managerOptions("proxy-system", nil).Metrics.BindAddress }},
		{"enable-pprof", "true", ":8082", func(c *cliOptions) any { return c.managerOptions("proxy-system", nil).PprofBindAddress }},
		{"capsule-configuration-name", "custom", "custom", func(c *cliOptions) any { return c.capsuleConfigurationController(nil).CapsuleConfigurationName }},
		{"client-connection-qps", "47.5", float32(47.5), func(c *cliOptions) any { cfg := &rest.Config{}; c.configureClient(cfg); return cfg.QPS }},
		{"client-connection-burst", "79", 79, func(c *cliOptions) any { cfg := &rest.Config{}; c.configureClient(cfg); return cfg.Burst }},
	}
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			c := parseCLI(t, "--"+tt.flag+"="+tt.value)
			if got := tt.read(c); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("runtime option = %#v, want %#v", got, tt.want)
			}
		})
	}
	defaults := parseCLI(t)
	cfg := &rest.Config{QPS: 999, Burst: 999}
	defaults.configureClient(cfg)
	if cfg.QPS != 20 || cfg.Burst != 30 {
		t.Fatalf("default client limits = %v/%v", cfg.QPS, cfg.Burst)
	}
	manager := defaults.managerOptions("proxy-system", runtime.NewScheme())
	if manager.Metrics.BindAddress != ":8080" || manager.PprofBindAddress != "" || manager.HealthProbeBindAddress != ":8081" || manager.LeaderElectionNamespace != "proxy-system" {
		t.Fatalf("unexpected default manager options: %+v", manager)
	}
}

type cliManager struct {
	ctrl.Manager
	cached client.Client
	direct client.Reader
}

func (m cliManager) GetClient() client.Client    { return m.cached }
func (m cliManager) GetAPIReader() client.Reader { return m.direct }

func TestCLICacheAndReflector(t *testing.T) {
	t.Parallel()
	mgr := cliManager{cached: fake.NewClientBuilder().Build(), direct: fake.NewClientBuilder().Build()}
	for _, disable := range []bool{false, true} {
		for _, reflector := range []bool{false, true} {
			t.Run(fmt.Sprintf("cache-disabled=%t/reflector=%t", disable, reflector), func(t *testing.T) {
				c := parseCLI(t, fmt.Sprintf("--disable-caching=%t", disable), fmt.Sprintf("--enable-reflector=%t", reflector), "--rolebindings-resync-period=37m")
				wantReader := client.Reader(mgr.cached)
				if disable {
					wantReader = mgr.direct
				}
				if c.reader(mgr) != wantReader {
					t.Fatal("selected the wrong Kubernetes reader")
				}
				wantReflector := reflector && !disable
				if c.reflectorEnabled() != wantReflector {
					t.Fatal("incorrect reflector activation")
				}
				period := c.managerOptions("proxy-system", nil).Cache.SyncPeriod
				if wantReflector {
					if period == nil || *period != 37*time.Minute {
						t.Fatalf("resync period = %v", period)
					}
				} else if period != nil {
					t.Fatal("disabled reflector should not set a resync period")
				}
			})
		}
	}
	c := parseCLI(t, "--enable-reflector")
	if got := *c.managerOptions("proxy-system", nil).Cache.SyncPeriod; got != 10*time.Hour {
		t.Fatalf("default resync = %v", got)
	}
}

func TestCLIClientRateLimitSentinels(t *testing.T) {
	t.Parallel()
	for _, value := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			c := parseCLI(t, fmt.Sprintf("--client-connection-qps=%d", value), fmt.Sprintf("--client-connection-burst=%d", value))
			config := &rest.Config{Host: "https://kubernetes.example", QPS: 99, Burst: 99}
			c.configureClient(config)
			if config.QPS != float32(value) || config.Burst != value || config.Host != "https://kubernetes.example" {
				t.Fatalf("client-go sentinel values were not preserved: %v/%v", config.QPS, config.Burst)
			}
		})
	}
}

// These assertions characterize existing no-ops, not the behavior promised by
// help text. Changing them should be a deliberate, separately reviewed fix.
func TestCLILegacyManagerNoOps(t *testing.T) {
	t.Parallel()
	c := parseCLI(t, "--enable-leader-election", "--webhook-port=10443")
	if !c.enableLeaderElection || c.webhookPort != 10443 {
		t.Fatal("flags were not parsed")
	}
	mgr := c.managerOptions("proxy-system", nil)
	if mgr.LeaderElection {
		t.Fatal("legacy leader-election behavior changed")
	}
	if mgr.WebhookServer != nil {
		t.Fatal("webhook-port started configuring a previously unused server")
	}
}

func TestCLIFeatureGates(t *testing.T) {
	t.Parallel()
	featuresUnderTest := []featuregate.Feature{features.ProxyAllNamespaced, features.ProxyClusterScoped, features.SkipImpersonationReview}
	for _, feature := range featuresUnderTest {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s=%t", feature, enabled), func(t *testing.T) {
				c := parseCLI(t, fmt.Sprintf("--feature-gates=%s=%t", feature, enabled))
				for _, other := range featuresUnderTest {
					if got := c.gates.Enabled(other); got != (other == feature && enabled) {
						t.Fatalf("%s = %t", other, got)
					}
				}
				if cliListener(t, c).SkipImpersonationReview() != (feature == features.SkipImpersonationReview && enabled) {
					t.Fatal("impersonation gate not wired")
				}
			})
		}
	}
	all := parseCLI(t, "--feature-gates=AllAlpha=true")
	for _, feature := range featuresUnderTest {
		if !all.gates.Enabled(feature) {
			t.Fatalf("AllAlpha did not enable %s", feature)
		}
		if parseCLI(t).gates.Enabled(feature) {
			t.Fatalf("%s leaked into a subsequent parse", feature)
		}
	}
}

func TestCLIRepeatedAndEmptyFlags(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"allowed-paths", "public-paths", "ignored-user-group", "ignored-username", "ignored-impersonation-group", "trusted-proxy-cidrs"} {
		t.Run(name, func(t *testing.T) {
			_, fs := cliFlagSet()
			if err := fs.Parse([]string{"--" + name + "=first,second", "--" + name + "=third"}); err != nil {
				t.Fatal(err)
			}
			got, err := fs.GetStringSlice(name)
			if err != nil || !reflect.DeepEqual(got, []string{"first", "second", "third"}) {
				t.Fatalf("repeated slice = %v: %v", got, err)
			}
			_, empty := cliFlagSet()
			if err := empty.Parse([]string{"--" + name + "="}); err != nil {
				t.Fatal(err)
			}
			values, err := empty.GetStringSlice(name)
			if err != nil || len(values) != 0 {
				t.Fatalf("empty slice = %v: %v", values, err)
			}
		})
	}
	c := parseCLI(t, "--auth-preferred-types=BearerToken", "--auth-preferred-types=TLSCertificate,AuthType(3)")
	if want := []request.AuthType{request.BearerToken, request.TLSCertificate, request.XForwardedClientCert}; !reflect.DeepEqual(c.authTypes, want) {
		t.Fatalf("authentication order = %v", c.authTypes)
	}
	if got := parseCLI(t, "--metrics-addr=:9090", "--metrics-addr=0").managerOptions("", nil).Metrics.BindAddress; got != "0" {
		t.Fatalf("last scalar value = %q", got)
	}
}

func TestCLIInvalidArguments(t *testing.T) {
	t.Parallel()
	for _, arg := range []string{
		"--unknown-flag=true", "--listening-port=-1", "--listening-port=abc", "--webhook-port=abc",
		"--client-connection-qps=abc", "--client-connection-burst=2147483648", "--client-connection-burst=1.5",
		"--rolebindings-resync-period=10", "--auth-preferred-types=Anonymous", "--auth-preferred-types=bearertoken",
		// Currently advertised but rejected; retain until explicitly fixed.
		"--auth-preferred-types=XForwardedClientCert", "--zap-time-encoding=nano",
		"--feature-gates=Unknown=true", "--feature-gates=ProxyClusterScoped=maybe",
		"--zap-encoder=yaml", "--zap-log-level=verbose", "--zap-stacktrace-level=debug", "--zap-time-encoding=clock",
		"--metrics-addr",
	} {
		t.Run(arg, func(t *testing.T) {
			_, fs := cliFlagSet()
			if err := fs.Parse([]string{arg}); err == nil {
				t.Fatal("invalid argument accepted")
			}
		})
	}
	for _, name := range []string{"disable-caching", "enable-leader-election", "enable-pprof", "enable-reflector", "enable-ssl", "zap-devel"} {
		t.Run(name, func(t *testing.T) {
			_, fs := cliFlagSet()
			if err := fs.Parse([]string{"--" + name + "=perhaps"}); err == nil {
				t.Fatal("invalid boolean accepted")
			}
			for _, value := range []string{"true", "false"} {
				_, valid := cliFlagSet()
				if err := valid.Parse([]string{"--" + name + "=" + value}); err != nil {
					t.Fatal(err)
				}
				got, err := valid.GetBool(name)
				if err != nil || got != (value == "true") {
					t.Fatalf("boolean = %t: %v", got, err)
				}
			}
		})
	}
	for _, args := range [][]string{
		{"--impersonation-group-regexp=["},
		{"--trusted-proxy-cidrs=not-a-cidr"},
		{"--trusted-proxy-cidrs=10.0.0.0/8,invalid"},
		{"--public-paths=/api"},
		{"--allowed-paths=/custom", "--public-paths=/custom"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseCLI(t, args...).listenerOptions(&rest.Config{Host: "https://kubernetes.example"}); err == nil {
				t.Fatal("invalid listener configuration accepted")
			}
		})
	}
}

func cliCertificate(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "certificate-user", Organization: []string{"certificate-group"}}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestCLIServerTLSAndPort(t *testing.T) {
	t.Parallel()
	certPEM, keyPEM := cliCertificate(t)
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	config := &rest.Config{TLSClientConfig: rest.TLSClientConfig{CAData: certPEM}}
	for _, tt := range []struct {
		name      string
		args      []string
		tls       bool
		port      uint
		errorText string
	}{
		{"TLS default port", []string{"--ssl-cert-path=" + certPath, "--ssl-key-path=" + keyPath}, true, 9001, ""},
		{"TLS custom port", []string{"--ssl-cert-path=" + certPath, "--ssl-key-path=" + keyPath, "--listening-port=9444"}, true, 9444, ""},
		{"HTTP", []string{"--enable-ssl=false", "--listening-port=9080"}, false, 9080, ""},
		{"missing default certificate", nil, true, 0, "TLS certificate file"},
		{"missing key", []string{"--ssl-cert-path=" + certPath}, true, 0, "TLS certificate key file"},
		{"missing cert file", []string{"--ssl-cert-path=" + filepath.Join(dir, "missing"), "--ssl-key-path=" + keyPath}, true, 0, "TLS certificate file"},
		{"HTTP with cert", []string{"--enable-ssl=false", "--ssl-cert-path=" + certPath}, false, 0, "cannot use a Certificate when"},
		{"HTTP with key", []string{"--enable-ssl=false", "--ssl-key-path=" + keyPath}, false, 0, "cannot use a Certificate key when"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseCLI(t, tt.args...).serverOptions(config)
			if tt.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errorText) {
					t.Fatalf("error = %v, want %q", err, tt.errorText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if o.IsListeningTLS() != tt.tls || o.ListeningPort() != tt.port {
				t.Fatalf("server TLS/port = %t/%d", o.IsListeningTLS(), o.ListeningPort())
			}
			if tt.tls && (o.TLSCertificatePath() != certPath || o.TLSCertificateKeyPath() != keyPath) {
				t.Fatal("certificate paths not wired")
			}
			if o.GetCertificateAuthorityPool() == nil {
				t.Fatal("missing client certificate CA pool")
			}
		})
	}
}

type cliReviews struct {
	client.Client
	allow   bool
	reviews int
}

func (c *cliReviews) Create(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
	switch review := obj.(type) {
	case *authenticationv1.TokenReview:
		review.Status.Authenticated = true
		review.Status.User = authenticationv1.UserInfo{Username: "token-user", Groups: []string{"token-group"}}
	case *authorizationv1.SubjectAccessReview:
		c.reviews++
		review.Status.Allowed = c.allow
	default:
		return fmt.Errorf("unexpected review %T", obj)
	}
	return nil
}

func cliRequest(t *testing.T, c *cliOptions, r *http.Request, writer client.Writer) request.Request {
	t.Helper()
	o := cliListener(t, c)
	return request.NewHTTP(r, o.AuthTypes(), o.PreferredUsernameClaim(), writer, o.IgnoredImpersonationsGroups(), o.ImpersonationGroupsRegexp(), o.SkipImpersonationReview(), o.XFCCHeader())
}

func TestCLIAuthenticationPrecedenceAndHeader(t *testing.T) {
	t.Parallel()
	certPEM, _ := cliCertificate(t)
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name         string
		args         []string
		header, want string
	}{
		{"default TLS first", nil, "", "certificate-user"},
		{"token first", []string{"--auth-preferred-types=BearerToken,TLSCertificate"}, "", "token-user"},
		{"TLS disabled", []string{"--auth-preferred-types=BearerToken"}, "", "token-user"},
		{"forwarded default header", []string{"--auth-preferred-types=AuthType(3)"}, "X-Forwarded-Client-Cert", "certificate-user"},
		{"forwarded custom header", []string{"--auth-preferred-types=AuthType(3)", "--xfcc-header-name=X-Client-Cert"}, "X-Client-Cert", "certificate-user"},
		{"wrong forwarded header", []string{"--auth-preferred-types=AuthType(3)", "--xfcc-header-name=X-Client-Cert"}, "X-Forwarded-Client-Cert", ""},
		// TokenReview supplies identity; this legacy flag does not inspect JWT claims.
		{"OIDC claim currently inert", []string{"--auth-preferred-types=BearerToken", "--oidc-username-claim=email"}, "", "token-user"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil)
			r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
			r.Header.Set("Authorization", "Bearer test-token")
			if tt.header != "" {
				r.Header.Set(tt.header, "Cert=\""+url.QueryEscape(string(certPEM))+"\"")
			}
			username, _, err := cliRequest(t, parseCLI(t, tt.args...), r, &cliReviews{}).GetUserAndGroups()
			if tt.want == "" {
				if err == nil {
					t.Fatal("authentication unexpectedly succeeded")
				}
			} else if err != nil || username != tt.want {
				t.Fatalf("identity = %q, %v; want %q", username, err, tt.want)
			}
		})
	}
}

func TestCLIImpersonationFilteringAndReview(t *testing.T) {
	t.Parallel()
	for _, skip := range []bool{false, true} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("skip=%t/allowed=%t", skip, allow), func(t *testing.T) {
				c := parseCLI(t, "--auth-preferred-types=BearerToken", "--impersonation-group-regexp=^tenant:", "--ignored-impersonation-group=tenant:admin", fmt.Sprintf("--feature-gates=SkipImpersonationReview=%t", skip))
				r := httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil)
				r.Header.Set("Authorization", "Bearer test-token")
				r.Header.Set("Impersonate-User", "tenant-user")
				for _, group := range []string{"tenant:readers", "tenant:admin", "system:masters"} {
					r.Header.Add("Impersonate-Group", group)
				}
				reviews := &cliReviews{allow: allow}
				username, groups, err := cliRequest(t, c, r, reviews).GetUserAndGroups()
				if !allow && !skip {
					if err == nil || reviews.reviews == 0 {
						t.Fatal("denied impersonation must fail after a review")
					}
					return
				}
				if err != nil || username != "tenant-user" || !reflect.DeepEqual(groups, []string{"tenant:readers"}) {
					t.Fatalf("identity = %q/%v: %v", username, groups, err)
				}
				wantReviews := 2
				if skip {
					wantReviews = 0
				}
				if reviews.reviews != wantReviews {
					t.Fatalf("reviews = %d, want %d", reviews.reviews, wantReviews)
				}
			})
		}
	}
}

func TestCLIIgnoredIdentities(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		args   []string
		bypass bool
	}{
		{"default", nil, false},
		{"matching username", []string{"--ignored-username=token-user"}, true},
		{"different username", []string{"--ignored-username=other"}, false},
		{"matching group", []string{"--ignored-user-group=token-group"}, true},
		{"different group", []string{"--ignored-user-group=other"}, false},
		{"either matches", []string{"--ignored-username=other", "--ignored-user-group=token-group"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			o := cliListener(t, parseCLI(t, tt.args...))
			bypassed := false
			handler := middleware.CheckUserInIgnoredIdentityMiddleware(&cliReviews{}, logr.Discard(), o.PreferredUsernameClaim(), o.AuthTypes(), sets.New(o.IgnoredUsernames()...), sets.New(o.IgnoredGroupNames()...), o.IgnoredImpersonationsGroups(), o.ImpersonationGroupsRegexp(), o.SkipImpersonationReview(), o.XFCCHeader(), func(http.ResponseWriter, *http.Request) { bypassed = true })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			r := httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil)
			r.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != http.StatusOK || bypassed != tt.bypass {
				t.Fatalf("bypass = %t, status = %d", bypassed, response.Code)
			}
		})
	}
}

func TestCLIPathAndTrustedSourceMiddleware(t *testing.T) {
	t.Parallel()
	c := parseCLI(t, "--allowed-paths=/discovery", "--public-paths=/live", "--trusted-proxy-cidrs=10.0.0.0/8,2001:db8::/32")
	o := cliListener(t, c)
	for _, tt := range []struct {
		path, remote, want string
		status             int
	}{
		{"/live", "10.1.2.3:443", "public", 200},
		{"/discovery", "[2001:db8::1]:443", "allowed", 200},
		{"/api", "10.1.2.3:443", "filtered", 200},
		{"/live/child", "10.1.2.3:443", "filtered", 200},
		{"/live", "192.0.2.1:443", "", 403},
		{"/discovery", "not-an-address", "", 403},
	} {
		t.Run(tt.path+"/"+tt.remote, func(t *testing.T) {
			var destination string
			handler := middleware.RequireTrustedSourceMiddleware(logr.Discard(), o.TrustedProxyCIDRs())(
				middleware.CheckPaths(logr.Discard(), sets.New(o.PublicPaths()...), func(http.ResponseWriter, *http.Request) { destination = "public" })(
					middleware.CheckPaths(logr.Discard(), sets.New(o.AllowedPaths()...), func(http.ResponseWriter, *http.Request) { destination = "allowed" })(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destination = "filtered" }))))
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.RemoteAddr = tt.remote
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, r)
			if response.Code != tt.status || destination != tt.want {
				t.Fatalf("destination/status = %q/%d", destination, response.Code)
			}
		})
	}
}

func TestCLILogging(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name               string
		args               []string
		debug, json, stack bool
	}{
		{"defaults", nil, false, true, false},
		{"development", []string{"--zap-devel"}, true, false, false},
		{"JSON overrides development", []string{"--zap-devel", "--zap-encoder=json"}, true, true, false},
		{"console", []string{"--zap-encoder=console"}, false, false, false},
		{"debug level", []string{"--zap-log-level=debug"}, true, true, false},
		{"numeric verbosity", []string{"--zap-log-level=4"}, true, true, false},
		{"info stacktraces", []string{"--zap-stacktrace-level=info"}, false, true, true},
		{"panic stacktraces", []string{"--zap-stacktrace-level=panic"}, false, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := parseCLI(t, tt.args...)
			var output bytes.Buffer
			logger := zap.New(zap.UseFlagOptions(&c.logOptions), zap.WriteTo(&output))
			logger.V(1).Info("debug-message")
			logger.Info("info-message")
			if strings.Contains(output.String(), "debug-message") != tt.debug {
				t.Fatalf("verbosity mismatch: %s", output.String())
			}
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			if json.Valid([]byte(lines[0])) != tt.json {
				t.Fatalf("encoder mismatch: %s", output.String())
			}
			if strings.Contains(output.String(), "stacktrace") != tt.stack {
				t.Fatalf("stacktrace mismatch: %s", output.String())
			}
		})
	}
	// The existing ISO8601 encoder override wins even over zap-time-encoding.
	for _, encoding := range []string{"epoch", "millis", "nanos", "iso8601", "rfc3339", "rfc3339nano"} {
		t.Run("time/"+encoding, func(t *testing.T) {
			c := parseCLI(t, "--zap-time-encoding="+encoding)
			var output bytes.Buffer
			zap.New(zap.UseFlagOptions(&c.logOptions), zap.WriteTo(&output)).Info("timestamp")
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			stamp, ok := entry["ts"].(string)
			if !ok {
				t.Fatalf("legacy timestamp override changed: %v", entry["ts"])
			}
			if _, err := time.Parse("2006-01-02T15:04:05.000Z0700", stamp); err != nil {
				t.Fatal(err)
			}
		})
	}
}
