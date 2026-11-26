package multicluster

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// Kubeconfig TTL guardrails (G2 §1, T-M3-10, DEV_TASK_CHECKLIST v1.3):
// - Default TTL: 8h
// - Hard max TTL: 24h
// - cluster-admin hard max TTL: 4h and NEVER uses O=system:masters (uses revocable kinstall:cluster-admin!)
const (
	DefaultKubeconfigTTL = 8 * time.Hour
	MaxKubeconfigTTL     = 24 * time.Hour
	MaxAdminTTL          = 4 * time.Hour
)

var roleToK8sGroup = map[string]string{
	"cluster-admin": "kinstall:cluster-admin", // NEVER system:masters!
	"ns-admin":      "kinstall:ns-admin",
	"edit":          "kinstall:edit",
	"view":          "kinstall:view",
}

// KubeconfigManager issues and revokes short-lived RBAC kubeconfigs across managed clusters (G2 §1).
type KubeconfigManager struct {
	repo  ports.KubeconfigRepo
	pki   ports.PKIIssuer
	audit ports.AuditRecorder
}

func NewKubeconfigManager(repo ports.KubeconfigRepo, pkiIssuer ports.PKIIssuer, audit ports.AuditRecorder) *KubeconfigManager {
	return &KubeconfigManager{repo: repo, pki: pkiIssuer, audit: audit}
}

type IssueRequest struct {
	InstanceID  string
	ClusterName string
	APIServer   string
	UserID      string
	Username    string
	K8sRole     string // cluster-admin | ns-admin | edit | view
	Namespaces  []string
	TTL         time.Duration
}

type IssuedKubeconfig struct {
	Record         *domain.ClusterKubeconfig `json:"record"`
	KubeconfigYAML string                    `json:"kubeconfigYaml"`
}

func (m *KubeconfigManager) Issue(ctx context.Context, req IssueRequest) (*IssuedKubeconfig, error) {
	group, ok := roleToK8sGroup[req.K8sRole]
	if !ok {
		return nil, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusUnprocessableEntity, sdk.CodeAPIInvalidRequest, false,
			fmt.Errorf("unsupported k8s_role %q", req.K8sRole))
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = DefaultKubeconfigTTL
	}
	if req.K8sRole == "cluster-admin" && ttl > MaxAdminTTL {
		ttl = MaxAdminTTL
	}
	if ttl > MaxKubeconfigTTL {
		ttl = MaxKubeconfigTTL
	}
	cert, err := m.pki.Issue(ctx, ports.CertRequest{
		CAScope:    "k8s-" + req.InstanceID,
		CommonName: fmt.Sprintf("kinstall-user:%s", req.Username),
		Orgs:       []string{group},
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		TTL:        ttl,
		KeyAlg:     "ecdsa-p256",
	})
	if err != nil {
		return nil, err
	}
	rec := &domain.ClusterKubeconfig{
		ID:         domain.NewID(domain.PrefixKubeconfig),
		InstanceID: req.InstanceID,
		UserID:     req.UserID,
		K8sRole:    req.K8sRole,
		Namespaces: req.Namespaces,
		CertSerial: cert.Serial,
		ExpiresAt:  cert.NotAfter,
	}
	if err := m.repo.Create(ctx, rec); err != nil {
		return nil, err
	}
	apiSrv := req.APIServer
	if apiSrv == "" {
		apiSrv = "https://127.0.0.1:6443"
	}
	kcYAML := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: %s@%s
  context:
    cluster: %s
    user: %s
current-context: %s@%s
users:
- name: %s
  user:
    client-certificate-data: %s
    client-key-data: %s
`, req.ClusterName, apiSrv, base64.StdEncoding.EncodeToString(cert.CAPEM),
		req.Username, req.ClusterName, req.ClusterName, req.Username,
		req.Username, req.ClusterName, req.Username,
		base64.StdEncoding.EncodeToString(cert.CertPEM),
		base64.StdEncoding.EncodeToString(cert.KeyPEM),
	)
	if m.audit != nil {
		_, _ = m.audit.Append(ctx, domain.AuditEntry{
			ActorID: req.UserID, ActorType: "user", Action: "cluster.kubeconfig.issue",
			TargetType: "instance", TargetID: req.InstanceID, Result: "success",
			PayloadJSON: fmt.Sprintf(`{"kubeconfigId":%q,"role":%q,"group":%q,"ttlSeconds":%d}`, rec.ID, req.K8sRole, group, int(ttl.Seconds())),
		})
	}
	return &IssuedKubeconfig{Record: rec, KubeconfigYAML: kcYAML}, nil
}

func (m *KubeconfigManager) Revoke(ctx context.Context, kcID, revokedBy string) error {
	if err := m.repo.Revoke(ctx, kcID, revokedBy); err != nil {
		return err
	}
	if m.audit != nil {
		_, _ = m.audit.Append(ctx, domain.AuditEntry{
			ActorID: revokedBy, ActorType: "user", Action: "cluster.kubeconfig.revoke",
			TargetType: "kubeconfig", TargetID: kcID, Result: "success",
			PayloadJSON: fmt.Sprintf(`{"kubeconfigId":%q}`, kcID),
		})
	}
	return nil
}
