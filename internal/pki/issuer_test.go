package pki_test

import (
	"context"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/pki"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestPKICAIssueVerifyAndRejectEd25519(t *testing.T) {
	issuer := pki.NewIssuer()
	ctx := context.Background()

	caPEM, _, err := issuer.EnsureCA(ctx, "k8s-prod", "kubernetes-ca")
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	issued, err := issuer.Issue(ctx, ports.CertRequest{
		CAScope:    "k8s-prod",
		CommonName: "kube-apiserver",
		DNSNames:   []string{"kubernetes", "kubernetes.default.svc", "k8s.corp.local"},
		IPs:        []net.IP{net.ParseIP("10.96.0.1"), net.ParseIP("10.0.1.10"), net.ParseIP("fd00:10::1")},
		TTL:        365 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to parse CA PEM")
	}
	leaf, err := pki.ParseCertPEM(issued.CertPEM)
	if err != nil {
		t.Fatalf("ParseCertPEM: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:   "k8s.corp.local",
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("certificate chain verify failed: %v", err)
	}

	renew, err := pki.NeedsRenewal(issued.CertPEM, 0.30)
	if err != nil || renew {
		t.Fatalf("fresh cert should not need renewal: renew=%v err=%v", renew, err)
	}

	// Ed25519 must be rejected with KI-PKI-0001
	if _, err := issuer.Issue(ctx, ports.CertRequest{CAScope: "k8s-prod", CommonName: "bad", KeyAlg: "ed25519"}); !sdk.IsCode(err, sdk.CodePKIInvalidRequest) {
		t.Fatalf("expected KI-PKI-0001 for ed25519, got %v", err)
	}
}
