package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"al.essio.dev/pkg/shellescape"

	"github.com/kinstall/kinstall/internal/artifact/bundle"
	"github.com/kinstall/kinstall/internal/artifact/catalog"
	"github.com/kinstall/kinstall/internal/authz"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/executor/sshx"
	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// WithArtifactStore attaches the local offline artifact repository store to Facade.
func (f *Facade) WithArtifactStore(st *catalog.Store) *Facade {
	f.artifacts = st
	return f
}

// WithSSHPool attaches the shared SSH connection pool for interactive Web SSH PTY terminals.
func (f *Facade) WithSSHPool(pool *sshx.Pool) *Facade {
	f.sshPool = pool
	return f
}

func (f *Facade) ensureArtifactStore() *catalog.Store {
	if f.artifacts != nil {
		return f.artifacts
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	f.artifacts = catalog.NewStore(filepath.Join(home, ".kinstall", "artifacts"), "/var/lib/kinstall/artifacts")
	return f.artifacts
}

func (f *Facade) projectLocalRoots(ctx context.Context, projectID string) ([]string, bool) {
	var roots []string
	offlineStrict := false
	if f.store == nil {
		return roots, offlineStrict
	}
	policies, err := f.store.MediaPolicies().List(ctx, projectID)
	if err != nil || len(policies) == 0 {
		return roots, offlineStrict
	}
	p := policies[0]
	offlineStrict = p.OfflineStrict
	for _, sc := range p.Sources {
		if strings.EqualFold(sc.Type, "local") {
			roots = append(roots, sc.Roots...)
		}
	}
	return roots, offlineStrict
}

// ProductReadiness summarizes offline artifact coverage for one built-in product.
type ProductReadiness struct {
	Product        string   `json:"product"`
	PluginName     string   `json:"pluginName"`
	DefaultVersion string   `json:"defaultVersion"`
	Targets        []string `json:"targets"`
	ReadyAMD64     bool     `json:"readyAmd64"`
	ReadyARM64     bool     `json:"readyArm64"`
	ArtifactCount  int      `json:"artifactCount"`
	ContainerReady bool     `json:"containerReady"`
}

// ArtifactCatalogResponse is returned by ListArtifacts for the Offline Supply Chain UI.
type ArtifactCatalogResponse struct {
	PrimaryRoot     string                  `json:"primaryRoot"`
	Roots           []string                `json:"roots"`
	OfflineStrict   bool                    `json:"offlineStrict"`
	TotalCount      int                     `json:"totalCount"`
	TotalBytes      int64                   `json:"totalBytes"`
	Items           []catalog.ArtifactEntry `json:"items"`
	ReadinessMatrix []ProductReadiness      `json:"readinessMatrix"`
}

// ListArtifacts scans local offline roots + primary store and computes plugin readiness coverage.
func (f *Facade) ListArtifacts(ctx context.Context, actor domain.Actor, projectID string) (*ArtifactCatalogResponse, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	store := f.ensureArtifactStore()
	policyRoots, offlineStrict := f.projectLocalRoots(ctx, projectID)
	items, err := store.Scan(policyRoots...)
	if err != nil {
		return nil, err
	}
	var totalBytes int64
	for _, it := range items {
		totalBytes += it.SizeBytes
	}

	var matrix []ProductReadiness
	if f.reg != nil {
		for _, meta := range f.reg.ListComponents() {
			for _, pr := range meta.Products {
				var targets []string
				containerReady := false
				for _, t := range pr.Targets {
					targets = append(targets, string(t))
					if t == sdk.TargetHostContainer {
						containerReady = true
					}
				}
				amd64, arm64, cnt := false, false, 0
				pLow := strings.ToLower(pr.Product)
				for _, it := range items {
					if strings.ToLower(it.Product) == pLow || strings.Contains(strings.ToLower(it.FileName), pLow) {
						cnt++
						if it.Arch == "amd64" || it.Arch == "noarch" {
							amd64 = true
						}
						if it.Arch == "arm64" || it.Arch == "noarch" {
							arm64 = true
						}
					}
				}
				matrix = append(matrix, ProductReadiness{
					Product:        pr.Product,
					PluginName:     meta.Name,
					DefaultVersion: pr.DefaultVer,
					Targets:        targets,
					ReadyAMD64:     amd64,
					ReadyARM64:     arm64,
					ArtifactCount:  cnt,
					ContainerReady: containerReady,
				})
			}
		}
		sort.Slice(matrix, func(i, j int) bool { return matrix[i].Product < matrix[j].Product })
	}

	roots := append([]string{store.PrimaryRoot()}, policyRoots...)
	return &ArtifactCatalogResponse{
		PrimaryRoot:     store.PrimaryRoot(),
		Roots:           roots,
		OfflineStrict:   offlineStrict,
		TotalCount:      len(items),
		TotalBytes:      totalBytes,
		Items:           items,
		ReadinessMatrix: matrix,
	}, nil
}

// UploadArtifact saves a verified offline package/binary/image/chart into the local store and records audit.
func (f *Facade) UploadArtifact(ctx context.Context, actor domain.Actor, projectID, category, product, version, arch, fileName string, r io.Reader) (*catalog.ArtifactEntry, error) {
	if err := authz.Authorize(actor, projectID, "media.write"); err != nil {
		return nil, err
	}
	entry, err := f.ensureArtifactStore().SaveUpload(category, product, version, arch, fileName, r)
	if err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "artifact.upload", "artifact", entry.LogicalPath, "success", "", map[string]any{
		"product": entry.Product, "version": entry.Version, "arch": entry.Arch, "size": entry.SizeBytes, "sha256": entry.SHA256,
	})
	return entry, nil
}

// PrefetchArtifact downloads a remote package into the local offline pool when offlineStrict is disabled.
func (f *Facade) PrefetchArtifact(ctx context.Context, actor domain.Actor, projectID, rawURL, category, product, version, arch, fileName string) (*catalog.ArtifactEntry, error) {
	if err := authz.Authorize(actor, projectID, "media.write"); err != nil {
		return nil, err
	}
	_, offlineStrict := f.projectLocalRoots(ctx, projectID)
	if offlineStrict {
		return nil, sdk.NewError(sdk.CodeSrcOfflineStrictDenied, "SRC", http.StatusForbidden, sdk.CodeSrcOfflineStrictDenied, false,
			fmt.Errorf("offlineStrict is enabled on project %s: online prefetch is blocked; upload offline packages directly or disable offlineStrict", projectID))
	}
	entry, err := f.ensureArtifactStore().PrefetchURL(ctx, rawURL, category, product, version, arch, fileName)
	if err != nil {
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "artifact.prefetch", "artifact", entry.LogicalPath, "success", "", map[string]any{
		"url": rawURL, "sha256": entry.SHA256, "size": entry.SizeBytes,
	})
	return entry, nil
}

// DeleteArtifact removes an artifact from the primary offline pool.
func (f *Facade) DeleteArtifact(ctx context.Context, actor domain.Actor, projectID, logicalPath string) error {
	if err := authz.Authorize(actor, projectID, "media.write"); err != nil {
		return err
	}
	if err := f.ensureArtifactStore().Delete(logicalPath); err != nil {
		return err
	}
	f.recordAudit(ctx, actor, projectID, "artifact.delete", "artifact", logicalPath, "success", "", nil)
	return nil
}

// ExportArtifactBOM builds a complete bundle.Manifest for offline delivery & verification.
func (f *Facade) ExportArtifactBOM(ctx context.Context, actor domain.Actor, projectID string) (*bundle.Manifest, error) {
	if err := authz.Authorize(actor, projectID, "resource.read"); err != nil {
		return nil, err
	}
	policyRoots, _ := f.projectLocalRoots(ctx, projectID)
	m, err := f.ensureArtifactStore().ExportBOM(policyRoots...)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PushImageArtifactToHost uploads an offline container image tarball (.tar/.tar.gz/.tar.zst) via SSH and runs `docker load`.
func (f *Facade) PushImageArtifactToHost(ctx context.Context, actor domain.Actor, projectID, logicalPath, hostID string) (map[string]any, error) {
	if err := authz.Authorize(actor, projectID, "host.write"); err != nil {
		return nil, err
	}
	if f.exec == nil {
		return nil, fmt.Errorf("executor unavailable")
	}
	policyRoots, _ := f.projectLocalRoots(ctx, projectID)
	absPath, err := f.ensureArtifactStore().ResolvePath(logicalPath, policyRoots...)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSrcNotFound, "SRC", http.StatusNotFound, sdk.CodeSrcNotFound, false, err)
	}
	h, err := f.store.Hosts().Get(ctx, hostID)
	if err != nil {
		return nil, err
	}
	target, err := BuildHostTarget(ctx, f.store, f.secrets, h)
	if err != nil {
		return nil, err
	}
	sess, err := f.exec.Connect(ctx, target)
	ZeroTargetAuth(&target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()

	file, err := os.Open(absPath) // #nosec G304 -- resolved via catalog.Store.ResolvePath
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	remoteTar := path.Join("/tmp", "kinstall-img-"+domain.NewID("i")+"-"+filepath.Base(absPath))
	sha, err := sess.Upload(ctx, file, remoteTar, 0o640, "")
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(remoteTar)
	loadCmd := fmt.Sprintf("docker load -i %s", shellescape.Quote(remoteTar))
	if strings.HasSuffix(low, ".zst") {
		loadCmd = fmt.Sprintf("zstd -dc %s | docker load", shellescape.Quote(remoteTar))
	} else if strings.HasSuffix(low, ".gz") || strings.HasSuffix(low, ".tgz") {
		loadCmd = fmt.Sprintf("gzip -dc %s | docker load", shellescape.Quote(remoteTar))
	}
	script := fmt.Sprintf("set -e; %s; rm -f %s", loadCmd, shellescape.Quote(remoteTar))
	res, err := sess.Run(ctx, ports.Command{Script: script, Sudo: true, Timeout: 5 * time.Minute})
	if err != nil {
		_, _ = sess.Run(ctx, ports.Command{Script: "rm -f " + shellescape.Quote(remoteTar), Sudo: true})
		return nil, err
	}
	f.recordAudit(ctx, actor, projectID, "artifact.image_load", "host", h.ID, "success", "", map[string]any{
		"logicalPath": logicalPath, "hostName": h.Name, "sha256": sha,
	})
	return map[string]any{
		"hostId":      h.ID,
		"hostName":    h.Name,
		"logicalPath": logicalPath,
		"sha256":      sha,
		"output":      strings.TrimSpace(res.Stdout),
	}, nil
}
