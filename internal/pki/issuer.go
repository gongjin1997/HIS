package pki

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

type caBundle struct {
	cert    *x509.Certificate
	key     crypto.Signer
	certPEM []byte
	keyPEM  []byte
}

// Issuer implements ports.PKIIssuer providing isolated per-scope CAs and X.509 leaf certificates (04 §8, 05 §3.7).
type Issuer struct {
	mu  sync.RWMutex
	cas map[string]*caBundle
}

// NewIssuer creates a lightweight in-process CA & X.509 issuer.
func NewIssuer() *Issuer {
	return &Issuer{
		cas: make(map[string]*caBundle),
	}
}

// LoadCA imports an existing CA PEM pair under scope (for persistence/rotation).
func (i *Issuer) LoadCA(scope string, certPEM, keyPEM []byte) error {
	cert, err := ParseCertPEM(certPEM)
	if err != nil {
		return err
	}
	key, err := parsePrivateKeyPEM(keyPEM)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.cas[scope] = &caBundle{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}
	return nil
}

// EnsureCA returns the existing CA for scope or generates a new 10-year ECDSA P-256 CA.
func (i *Issuer) EnsureCA(_ context.Context, scope, commonName string) ([]byte, []byte, error) {
	if scope == "" {
		return nil, nil, sdk.NewError(sdk.CodePKIInvalidRequest, "PKI", http.StatusUnprocessableEntity, sdk.CodePKIInvalidRequest, false, fmt.Errorf("empty CA scope"))
	}
	i.mu.RLock()
	if existing, ok := i.cas[scope]; ok {
		i.mu.RUnlock()
		return existing.certPEM, existing.keyPEM, nil
	}
	i.mu.RUnlock()

	if commonName == "" {
		commonName = "kinstall-" + scope + "-ca"
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"kinstall"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	i.mu.Lock()
	if existing, ok := i.cas[scope]; ok {
		i.mu.Unlock()
		return existing.certPEM, existing.keyPEM, nil
	}
	i.cas[scope] = &caBundle{cert: cert, key: priv, certPEM: certPEM, keyPEM: keyPEM}
	i.mu.Unlock()
	return certPEM, keyPEM, nil
}

// Issue signs a leaf certificate under req.CAScope with dual-stack IP/DNS SANs.
// Explicitly rejects Ed25519 keys when issuing K8s certificates (05 §3.7).
func (i *Issuer) Issue(ctx context.Context, req ports.CertRequest) (*ports.IssuedCert, error) {
	if req.CommonName == "" {
		return nil, sdk.NewError(sdk.CodePKIInvalidRequest, "PKI", http.StatusUnprocessableEntity, sdk.CodePKIInvalidRequest, false, fmt.Errorf("missing CommonName"))
	}
	alg := strings.ToLower(strings.TrimSpace(req.KeyAlg))
	if alg == "" {
		alg = "ecdsa-p256"
	}
	if alg == "ed25519" {
		return nil, sdk.NewError(sdk.CodePKIInvalidRequest, "PKI", http.StatusUnprocessableEntity, sdk.CodePKIInvalidRequest, false, fmt.Errorf("ed25519 keys are prohibited for Kubernetes/TLS certificates (05 §3.7)"))
	}

	if _, _, err := i.EnsureCA(ctx, req.CAScope, ""); err != nil {
		return nil, err
	}
	i.mu.RLock()
	ca := i.cas[req.CAScope]
	i.mu.RUnlock()

	var signer crypto.Signer
	var err error
	switch alg {
	case "ecdsa-p256":
		signer, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "rsa-2048":
		signer, err = rsa.GenerateKey(rand.Reader, 2048)
	default:
		return nil, sdk.NewError(sdk.CodePKIInvalidRequest, "PKI", http.StatusUnprocessableEntity, sdk.CodePKIInvalidRequest, false, fmt.Errorf("unsupported keyAlg %q", req.KeyAlg))
	}
	if err != nil {
		return nil, err
	}

	ttl := req.TTL
	if ttl <= 0 {
		ttl = 365 * 24 * time.Hour
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	usages := req.Usages
	if len(usages) == 0 {
		usages = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   req.CommonName,
			Organization: req.Orgs,
		},
		DNSNames:              req.DNSNames,
		IPAddresses:           req.IPs,
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(ttl),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           usages,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, signer.Public(), ca.key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(signer)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return &ports.IssuedCert{
		CertPEM:  certPEM,
		KeyPEM:   keyPEM,
		CAPEM:    ca.certPEM,
		Serial:   serial.Text(16),
		NotAfter: tmpl.NotAfter,
	}, nil
}

// ParseCertPEM decodes the first CERTIFICATE PEM block.
func ParseCertPEM(certPEM []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(certPEM)
	if blk == nil {
		return nil, fmt.Errorf("invalid PEM certificate")
	}
	return x509.ParseCertificate(blk.Bytes)
}

// NeedsRenewal returns true if remaining lifetime is <= thresholdRatio (e.g. 0.30, D2 §5.1 #11).
func NeedsRenewal(certPEM []byte, thresholdRatio float64) (bool, error) {
	c, err := ParseCertPEM(certPEM)
	if err != nil {
		return true, err
	}
	total := c.NotAfter.Sub(c.NotBefore)
	if total <= 0 {
		return true, nil
	}
	rem := time.Until(c.NotAfter)
	return float64(rem)/float64(total) <= thresholdRatio, nil
}

func parsePrivateKeyPEM(keyPEM []byte) (crypto.Signer, error) {
	blk, _ := pem.Decode(keyPEM)
	if blk == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("key does not implement crypto.Signer")
	}
	return signer, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}
