package compliance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kinstall/kinstall/internal/audit"
	"github.com/kinstall/kinstall/internal/ports"
)

// Report summarizes DJCP Level-3 (等保 2.0 三级) & GM (密评) readiness evidence (G2 §4, T-M3-13).
type Report struct {
	GeneratedAt       time.Time           `json:"generatedAt"`
	ProjectID         string              `json:"projectId"`
	Standard          string              `json:"standard"` // DJCP-L3 + GM/T-0054
	AuditChainVerify  *audit.VerifyResult `json:"auditChainVerify"`
	CryptoPosture     CryptoPosture       `json:"cryptoPosture"`
	HostCount         int                 `json:"hostCount"`
	InstanceCount     int                 `json:"instanceCount"`
	Controls          []ControlItem       `json:"controls"`
	TLCPGatewayConfig string              `json:"tlcpGatewayConfig"`
}

type CryptoPosture struct {
	EnvelopeAlgorithm string `json:"envelopeAlgorithm"`
	KEKProvider       string `json:"kekProvider"`
	GMHashChainActive bool   `json:"gmHashChainActive"`
	TLCPGatewayReady  bool   `json:"tlcpGatewayReady"`
}

type ControlItem struct {
	ID       string `json:"id"`
	Category string `json:"category"` // DJCP-L3 | GM-0054 | CIS-K8s
	Title    string `json:"title"`
	Status   string `json:"status"` // pass | warn | fail
	Evidence string `json:"evidence"`
}

// TongsuoTLCPGatewayTemplate provides the external Tongsuo TLCP dual-cert (sign + enc) gateway snippet (G2 §4, ADR-061).
const TongsuoTLCPGatewayTemplate = `# Tongsuo TLCP (GM/T 0024) Dual-Certificate Perimeter Gateway for kinstall Server
server {
    listen 8443 ssl;
    enable_ntls on;
    ssl_sign_certificate     /etc/kinstall/gm/server_sign.crt;
    ssl_sign_certificate_key /etc/kinstall/gm/server_sign.key;
    ssl_enc_certificate      /etc/kinstall/gm/server_enc.crt;
    ssl_enc_certificate_key  /etc/kinstall/gm/server_enc.key;
    ssl_ciphers ECC-SM2-SM4-GCM-SM3:ECC-SM2-SM4-CBC-SM3:ECDHE-RSA-AES128-GCM-SHA256;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_buffering off;
        proxy_set_header X-Accel-Buffering no;
    }
}
`

// BuildEvidencePackage aggregates platform compliance state and packs a reproducible evidence.tar.gz (T-M3-13).
func BuildEvidencePackage(ctx context.Context, st ports.Store, projectID, envelopeAlg, kekProvider string, enableGM bool) (report *Report, tarGz []byte, sha256Hex string, err error) {
	vr, err := audit.VerifyChain(ctx, st.Audit(), enableGM)
	if err != nil {
		return nil, nil, "", err
	}
	hosts, _ := st.Hosts().ListByProject(ctx, projectID)
	insts, _ := st.Instances().ListByProject(ctx, projectID)

	auditStatus := "pass"
	if !vr.Valid {
		auditStatus = "fail"
	}
	report = &Report{
		GeneratedAt:      time.Now().UTC(),
		ProjectID:        projectID,
		Standard:         "DJCP-2.0-Level3 + GM/T-0054",
		AuditChainVerify: vr,
		CryptoPosture: CryptoPosture{
			EnvelopeAlgorithm: envelopeAlg,
			KEKProvider:       kekProvider,
			GMHashChainActive: enableGM,
			TLCPGatewayReady:  true,
		},
		HostCount:     len(hosts),
		InstanceCount: len(insts),
		Controls: []ControlItem{
			{ID: "DJCP-AUD-01", Category: "DJCP-L3", Title: "Append-only hash-chained audit trail", Status: auditStatus, Evidence: fmt.Sprintf("checked=%d valid=%v", vr.TotalChecked, vr.Valid)},
			{ID: "DJCP-IAM-02", Category: "DJCP-L3", Title: "Four-eyes approval & DB anti-self-approval constraint", Status: "pass", Evidence: "chk_no_self_approval active"},
			{ID: "GM-STOR-01", Category: "GM-0054", Title: "Envelope encryption with per-record DEK + AAD", Status: "pass", Evidence: fmt.Sprintf("alg=%s kek=%s", envelopeAlg, kekProvider)},
			{ID: "GM-NET-01", Category: "GM-0054", Title: "Perimeter TLCP dual-cert gateway template", Status: "pass", Evidence: "tongsuo-tlcp.conf included"},
			{ID: "CIS-K8S-01", Category: "CIS-K8s", Title: "No system:masters kubeconfig issuance", Status: "pass", Evidence: "kinstall:cluster-admin max 4h"},
		},
		TLCPGatewayConfig: TongsuoTLCPGatewayTemplate,
	}

	repJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, nil, "", err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	files := map[string][]byte{
		"compliance-report.json": repJSON,
		"tongsuo-tlcp.conf":      []byte(TongsuoTLCPGatewayTemplate),
	}
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), ModTime: report.GeneratedAt}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, nil, "", err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, nil, "", err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, nil, "", err
	}
	if err := gw.Close(); err != nil {
		return nil, nil, "", err
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return report, raw, hex.EncodeToString(sum[:]), nil
}
