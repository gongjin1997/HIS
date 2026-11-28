package catalog

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/kinstall/kinstall/internal/iam"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/db"
	"github.com/kinstall/kinstall/pkg/platform/id"
	"github.com/kinstall/kinstall/pkg/platform/safefs"
)

const MaxArtifactBytes = 2 << 30 // 2 GiB

type Artifact struct {
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspaceId"`
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	Kind             string         `json:"kind"` // binary | package | oci_tar | chart | script
	Arch             string         `json:"arch"` // amd64 | arm64 | noarch
	SHA256           string         `json:"sha256"`
	SizeBytes        int64          `json:"sizeBytes"`
	StoragePath      string         `json:"storagePath"`
	SignatureEd25519 string         `json:"signatureEd25519"`
	OCIRef           string         `json:"ociRef"`
	Metadata         map[string]any `json:"metadata"`
	UploadedBy       string         `json:"uploadedBy"`
	CreatedAt        string         `json:"createdAt"`
}

type DedupStats struct {
	TotalArtifacts int   `json:"totalArtifacts"`
	UniqueBlobs    int   `json:"uniqueBlobs"`
	LogicalBytes   int64 `json:"logicalBytes"`
	PhysicalBytes  int64 `json:"physicalBytes"`
	SavedBytes     int64 `json:"savedBytes"`
}

type VerifyResult struct {
	ArtifactID     string `json:"artifactId"`
	FileExists     bool   `json:"fileExists"`
	SHA256Match    bool   `json:"sha256Match"`
	SignatureValid bool   `json:"signatureValid"`
	Verified       bool   `json:"verified"`
	Message        string `json:"message"`
	PublicKey      string `json:"publicKey"`
}

type OCIInspectResult struct {
	Digest       string   `json:"digest"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	Layers       int      `json:"layers"`
	Env          []string `json:"env"`
}

type OCIPushResult struct {
	ArtifactID   string `json:"artifactId"`
	TargetRef    string `json:"targetRef"`
	Digest       string `json:"digest"`
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Status       string `json:"status"`
}

type Service struct {
	store   *db.Store
	codec   *crypto.Codec
	iam     *iam.Service
	dataDir string
}

func NewService(store *db.Store, codec *crypto.Codec, iamSvc *iam.Service, dataDir string) *Service {
	return &Service{
		store:   store,
		codec:   codec,
		iam:     iamSvc,
		dataDir: dataDir,
	}
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *Service) casBlobPath(sha string) (string, error) {
	if len(sha) != 64 {
		return "", errors.New("invalid sha256 digest")
	}
	return safefs.SafeJoin(filepath.Join(s.dataDir, "cas", "sha256"), filepath.Join(sha[:2], sha[2:4], sha))
}

func (s *Service) EnsureSeeded(ctx context.Context, wsID string) error {
	var cnt int
	if err := s.store.QueryRow(ctx, `SELECT COUNT(1) FROM artifact_records WHERE workspace_id = ?`, wsID).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	initScript := `#!/usr/bin/env bash
set -euo pipefail
echo "[Kinstall] Linux Baseline Sysctl & Kernel Check"
uname -a
free -m | head -n 2
df -h / | tail -n 1
`
	_, _ = s.UploadArtifact(ctx, wsID, "system", "kinstall-linux-baseline.sh", "2.0.0", "script", "noarch", strings.NewReader(initScript))

	healthScript := `#!/usr/bin/env bash
set -euo pipefail
echo "[Kinstall] Port & Process Diagnostic Bundle"
ss -tlnp 2>/dev/null | head -n 20 || netstat -tlnp 2>/dev/null | head -n 20 || true
`
	_, _ = s.UploadArtifact(ctx, wsID, "system", "his-health-diag.sh", "2.1.0", "script", "noarch", strings.NewReader(healthScript))
	return nil
}

// UploadArtifact streams content into the CAS store with automatic deduplication and Ed25519 signing.
func (s *Service) UploadArtifact(ctx context.Context, wsID, actor, nameStr, version, kind, arch string, r io.Reader) (*Artifact, error) {
	nameStr = strings.TrimSpace(nameStr)
	if nameStr == "" {
		return nil, errors.New("制品名称不能为空")
	}
	if version == "" {
		version = "1.0.0"
	}
	if kind == "" {
		kind = "binary"
	}
	if arch == "" {
		arch = "amd64"
	}

	tmpDir := filepath.Join(s.dataDir, "cas", "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return nil, err
	}
	tmpFile, err := os.CreateTemp(tmpDir, "upload-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmpFile, hasher), io.LimitReader(r, MaxArtifactBytes+1))
	closeErr := tmpFile.Close()
	if err != nil {
		return nil, fmt.Errorf("写入制品临时文件失败: %w", err)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written > MaxArtifactBytes {
		return nil, errors.New("制品体积超过 2 GiB 安全上限")
	}

	shaHex := hex.EncodeToString(hasher.Sum(nil))
	blobPath, err := s.casBlobPath(shaHex)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(blobPath); errors.Is(statErr, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(blobPath), 0o750); err != nil {
			return nil, err
		}
		if err := os.Rename(tmpPath, blobPath); err != nil {
			return nil, fmt.Errorf("落盘 CAS 对象失败: %w", err)
		}
		cleanupTmp = false
	}

	meta := map[string]any{"deduped": !cleanupTmp}
	// If this is an OCI tar archive, auto-inspect architecture/os/digest in pure Go!
	if kind == "oci_tar" {
		if info, ierr := InspectOCITar(blobPath); ierr == nil {
			meta["ociDigest"] = info.Digest
			meta["ociOs"] = info.OS
			meta["ociArch"] = info.Architecture
			meta["ociLayers"] = info.Layers
			if info.Architecture != "" {
				arch = info.Architecture
			}
		}
	}

	signPayload := []byte(fmt.Sprintf("%s:%d:%s", shaHex, written, arch))
	sig := s.codec.SignBytes(signPayload)
	now := nowISO()
	art := &Artifact{
		ID:               id.New("art"),
		WorkspaceID:      wsID,
		Name:             nameStr,
		Version:          version,
		Kind:             kind,
		Arch:             arch,
		SHA256:           shaHex,
		SizeBytes:        written,
		StoragePath:      blobPath,
		SignatureEd25519: sig,
		Metadata:         meta,
		UploadedBy:       actor,
		CreatedAt:        now,
	}
	metaBytes, _ := json.Marshal(meta)
	_, err = s.store.Exec(ctx,
		`INSERT INTO artifact_records(id, workspace_id, name, version, kind, arch, sha256, size_bytes, storage_path, signature_ed25519, oci_ref, metadata_json, uploaded_by, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		art.ID, wsID, art.Name, art.Version, art.Kind, art.Arch, art.SHA256, art.SizeBytes, art.StoragePath, art.SignatureEd25519, "", string(metaBytes), actor, now,
	)
	if err != nil {
		return nil, err
	}
	return art, nil
}

func (s *Service) ListArtifacts(ctx context.Context, wsID string) ([]Artifact, *DedupStats, error) {
	rows, err := s.store.Query(ctx,
		`SELECT id, workspace_id, name, version, kind, arch, sha256, size_bytes, storage_path, signature_ed25519, oci_ref, metadata_json, uploaded_by, created_at
		 FROM artifact_records WHERE workspace_id = ? ORDER BY created_at DESC`,
		wsID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Artifact, 0)
	seenSHA := make(map[string]int64)
	stats := &DedupStats{}
	for rows.Next() {
		var (
			a       Artifact
			metaRaw string
		)
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.Version, &a.Kind, &a.Arch, &a.SHA256, &a.SizeBytes, &a.StoragePath, &a.SignatureEd25519, &a.OCIRef, &metaRaw, &a.UploadedBy, &a.CreatedAt); err != nil {
			return nil, nil, err
		}
		a.Metadata = make(map[string]any)
		_ = json.Unmarshal([]byte(metaRaw), &a.Metadata)
		out = append(out, a)
		stats.TotalArtifacts++
		stats.LogicalBytes += a.SizeBytes
		if _, ok := seenSHA[a.SHA256]; !ok {
			seenSHA[a.SHA256] = a.SizeBytes
			stats.UniqueBlobs++
			stats.PhysicalBytes += a.SizeBytes
		}
	}
	stats.SavedBytes = max(0, stats.LogicalBytes-stats.PhysicalBytes)
	return out, stats, rows.Err()
}

func (s *Service) GetArtifact(ctx context.Context, wsID, artifactID string) (*Artifact, error) {
	var (
		a       Artifact
		metaRaw string
	)
	err := s.store.QueryRow(ctx,
		`SELECT id, workspace_id, name, version, kind, arch, sha256, size_bytes, storage_path, signature_ed25519, oci_ref, metadata_json, uploaded_by, created_at
		 FROM artifact_records WHERE workspace_id = ? AND id = ?`,
		wsID, artifactID,
	).Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.Version, &a.Kind, &a.Arch, &a.SHA256, &a.SizeBytes, &a.StoragePath, &a.SignatureEd25519, &a.OCIRef, &metaRaw, &a.UploadedBy, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.Metadata = make(map[string]any)
	_ = json.Unmarshal([]byte(metaRaw), &a.Metadata)
	return &a, nil
}

func (s *Service) DeleteArtifact(ctx context.Context, wsID, artifactID string) error {
	_, err := s.store.Exec(ctx, `DELETE FROM artifact_records WHERE workspace_id = ? AND id = ?`, wsID, artifactID)
	return err
}

// VerifyArtifact performs fail-closed SHA-256 disk check AND Ed25519 signature verification.
func (s *Service) VerifyArtifact(ctx context.Context, wsID, artifactID string) (*VerifyResult, error) {
	a, err := s.GetArtifact(ctx, wsID, artifactID)
	if err != nil {
		return nil, errors.New("制品不存在")
	}
	res := &VerifyResult{
		ArtifactID: artifactID,
		PublicKey:  s.codec.PublicKeyBase64(),
	}
	f, err := os.Open(a.StoragePath)
	if err != nil {
		res.Message = "CAS 对象文件缺失，验签失败 (Fail-Closed)"
		return res, nil
	}
	defer func() { _ = f.Close() }()
	res.FileExists = true

	h := sha256.New()
	sz, err := io.Copy(h, f)
	if err != nil {
		res.Message = "读取对象失败: " + err.Error()
		return res, nil
	}
	actualSHA := hex.EncodeToString(h.Sum(nil))
	res.SHA256Match = actualSHA == a.SHA256 && sz == a.SizeBytes
	signPayload := []byte(fmt.Sprintf("%s:%d:%s", a.SHA256, a.SizeBytes, a.Arch))
	res.SignatureValid = s.codec.VerifyBytes(signPayload, a.SignatureEd25519)
	res.Verified = res.FileExists && res.SHA256Match && res.SignatureValid
	if res.Verified {
		res.Message = "SHA-256 摘要与 Ed25519 数字签名校验全部通过"
	} else {
		res.Message = "完整性或签名校验未通过，已拒绝放行 (Fail-Closed)"
	}
	return res, nil
}

// InspectOCITar inspects an OCI/Docker image tarball in pure Go with zero docker daemon.
func InspectOCITar(tarPath string) (*OCIInspectResult, error) {
	img, err := tarball.ImageFromPath(tarPath, nil)
	if err != nil {
		return nil, fmt.Errorf("解析 OCI/Docker 镜像包失败: %w", err)
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	env := cfg.Config.Env
	if env == nil {
		env = make([]string, 0)
	}
	return &OCIInspectResult{
		Digest:       digest.String(),
		OS:           cfg.OS,
		Architecture: cfg.Architecture,
		Layers:       len(layers),
		Env:          env,
	}, nil
}

// PushOCITar pushes an OCI tarball directly to Harbor/SWR in pure Go with fail-closed architecture verification.
func (s *Service) PushOCITar(ctx context.Context, wsID, artifactID, targetRef, expectedArch, credentialID string, insecureTLS bool) (*OCIPushResult, error) {
	verify, err := s.VerifyArtifact(ctx, wsID, artifactID)
	if err != nil {
		return nil, err
	}
	if !verify.Verified {
		return nil, fmt.Errorf("制品验签未通过，禁止推送: %s", verify.Message)
	}
	a, err := s.GetArtifact(ctx, wsID, artifactID)
	if err != nil {
		return nil, err
	}

	img, err := tarball.ImageFromPath(a.StoragePath, nil)
	if err != nil {
		return nil, fmt.Errorf("当前制品不是合法的 OCI/Docker 镜像 tar 包: %w", err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	actualArch := strings.ToLower(strings.TrimSpace(cfg.Architecture))
	expArch := strings.ToLower(strings.TrimSpace(expectedArch))
	if expArch != "" && actualArch != "" && actualArch != expArch {
		return nil, fmt.Errorf("ARCH_MISMATCH: 镜像实际架构为 %s，与目标集群期望架构 %s 不一致，已 Fail-Closed 阻断混推", actualArch, expArch)
	}

	var nameOpts []name.Option
	if insecureTLS {
		nameOpts = append(nameOpts, name.Insecure)
	}
	ref, err := name.ParseReference(strings.TrimSpace(targetRef), nameOpts...)
	if err != nil {
		return nil, fmt.Errorf("无效的目标镜像地址 %q: %w", targetRef, err)
	}

	var remoteOpts []remote.Option
	remoteOpts = append(remoteOpts, remote.WithContext(ctx))
	if credentialID != "" && s.iam != nil {
		cred, cerr := s.iam.ResolveCredential(ctx, wsID, credentialID)
		if cerr != nil {
			return nil, fmt.Errorf("读取镜像仓库凭据失败: %w", cerr)
		}
		remoteOpts = append(remoteOpts, remote.WithAuth(&authn.Basic{
			Username: cred.Username,
			Password: cred.Secret,
		}))
	}
	if insecureTLS {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- explicit opt-in for air-gapped private Harbor
		remoteOpts = append(remoteOpts, remote.WithTransport(tr))
	}

	if err := remote.Write(ref, img, remoteOpts...); err != nil {
		return nil, fmt.Errorf("原生 OCI 推送失败: %w", err)
	}
	digest, _ := img.Digest()
	_, _ = s.store.Exec(ctx, `UPDATE artifact_records SET oci_ref = ? WHERE id = ?`, targetRef, artifactID)
	return &OCIPushResult{
		ArtifactID:   artifactID,
		TargetRef:    targetRef,
		Digest:       digest.String(),
		Architecture: actualArch,
		OS:           cfg.OS,
		Status:       "PUSHED",
	}, nil
}

func (s *Service) ExportBOM(ctx context.Context, wsID string) (map[string]any, error) {
	items, stats, err := s.ListArtifacts(ctx, wsID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"generatedAt":      nowISO(),
		"workspaceId":      wsID,
		"ed25519PublicKey": s.codec.PublicKeyBase64(),
		"dedupStats":       stats,
		"artifacts":        items,
	}, nil
}
