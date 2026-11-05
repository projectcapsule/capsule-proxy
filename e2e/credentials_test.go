//go:build e2e

// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	capsulerbac "github.com/projectcapsule/capsule/pkg/api/rbac"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var _ = Describe("Generated client credentials", Serial, Label("credentials"), func() {
	It("embeds issued certificates and preserves tenant isolation for certificate-authenticated clients", func() {
		f := newAccessFixture()
		f.addTenant("alice", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, "alice", "admin")}, "alice")
		f.addTenant("bob", capsulerbac.OwnerListSpec{f.owner(capsulerbac.UserOwner, "bob", "admin")}, "bob")

		for _, user := range []string{"alice", "bob", "joe", "cluster-admin", "dave"} {
			path := filepath.Join("..", "hack", user+".kubeconfig")
			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()&0o077).To(BeZero(), "%s credentials must not be accessible to other users", user)
			config, err := clientcmd.BuildConfigFromFlags("", path)
			Expect(err).NotTo(HaveOccurred())
			// Assert lengths instead of data so a failure never prints a private key.
			Expect(len(config.CertData)).To(BeNumerically(">", 0), "%s client certificate must be embedded", user)
			Expect(len(config.KeyData)).To(BeNumerically(">", 0), "%s private key must be embedded", user)
			Expect(config.CertFile).To(BeEmpty())
			Expect(config.KeyFile).To(BeEmpty())
			certificate, err := tls.X509KeyPair(config.CertData, config.KeyData)
			Expect(err).NotTo(HaveOccurred(), "%s must have an issued certificate matching its key", user)
			leaf, err := x509.ParseCertificate(certificate.Certificate[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(leaf.Subject.CommonName).To(Equal(user))
		}

		for _, actor := range []struct{ user, other string }{{"alice", "bob"}, {"bob", "alice"}} {
			cl, err := loadKubeConfig(actor.user)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() ([]string, error) {
				list, err := cl.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
				if err != nil {
					return nil, err
				}
				names := make([]string, len(list.Items))
				for i := range list.Items {
					names[i] = list.Items[i].Name
				}

				return names, nil
			}, accessTimeout, defaultPollInterval).Should(ConsistOf(f.namespaces[actor.user]))
			_, err = cl.CoreV1().Namespaces().Get(context.Background(), f.namespaces[actor.other], metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%s must not see the other tenant's namespace: %v", actor.user, err)
		}
	})
})
