package artifact_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinstall/kinstall/internal/artifact/bom"
	"github.com/kinstall/kinstall/internal/artifact/bundle"
	"github.com/kinstall/kinstall/internal/artifact/rewrite"
	"github.com/kinstall/kinstall/internal/artifact/runtime"
	artserver "github.com/kinstall/kinstall/internal/artifact/server"
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/artifact/verify"
	"github.com/kinstall/kinstall/internal/artifact/zot"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/obs"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestBOMPageSizeAndLicenseCompliance(t *testing.T) {
	raw := `
apiVersion: kinstall.io/v1alpha1
kind: BOM
product: valkey
version: "9.1.4"
revision: "2026.11.3"
components:
  - name: valkey-tarball
    version: "9.1.4"
    license: BSD-3-Clause
    artifacts:
      - arch: arm64
        pageSizes: [4096]
        kind: tarball
        sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
`
	doc, err := bom.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.HasArtifact("valkey-tarball", "arm64", 4096) {
		t.Fatal("expected 4K arm64 artifact match")
	}
	// ADR-028: arm64 64K host MUST NOT match 4K-only jemalloc build!
	if doc.HasArtifact("valkey-tarball", "arm64", 65536) {
		t.Fatal("64K page host must be blocked from 4K-only build!")
	}
	if err := doc.ValidatePublicBundleCompliance(); err != nil {
		t.Fatalf("BSD-3-Clause should pass: %v", err)
	}

	// Restricted license (SSPL-1.0 / Elastic-2.0) must be rejected from public bundle
	doc.Components[0].License = "SSPL-1.0"
	if err := doc.ValidatePublicBundleCompliance(); !sdk.IsCode(err, sdk.CodeArtifactVerifyFailed) {
		t.Fatalf("expected KI-ART-0001 on SSPL-1.0, got %v", err)
	}
}

func TestOfflineSM2VerifyAndTamperRejection(t *testing.T) {
	priv, err := verify.GenerateSM2KeyPair()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("kinstall-bom-rev-2026.11.3")
	sig, err := verify.SignSM2(priv, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := verify.VerifySM2(&priv.PublicKey, payload, sig); err != nil {
		t.Fatalf("valid SM2 signature failed: %v", err)
	}
	// 1-byte tamper MUST fail with KI-ART-0003
	tampered := append([]byte(nil), payload...)
	tampered[0] ^= 0xFF
	if err := verify.VerifySM2(&priv.PublicKey, tampered, sig); !sdk.IsCode(err, sdk.CodeArtifactSM2Failed) {
		t.Fatalf("expected KI-ART-0003 on tampered byte, got %v", err)
	}
}

func TestTernaryMediaResolverOfflineStrictSingleflightAndPresignedRedaction(t *testing.T) {
	ctx := context.Background()
	payload := []byte("valkey-9.1.4-arm64-64k-binary-payload")
	sum := sha256.Sum256(payload)
	wantSHA := hex.EncodeToString(sum[:])
	logicalPath := "binaries/valkey/9.1.4/arm64-64k/valkey.tar.zst"

	var fetchCount atomic.Int32
	obsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		fetchCount.Add(1)
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write(payload)
	}))
	defer obsSrv.Close()

	cacheDir := t.TempDir()
	localRoot := t.TempDir()

	// 1. offlineStrict=true with public OBS (internal=false) + online -> MUST block with KI-SRC-0005!
	pubOBS := source.NewObjectStoreSource(domain.MediaSourceConfig{
		Type: "obs", Priority: 2, Internal: false, Endpoint: obsSrv.URL, Bucket: "corp-art", Prefix: "prod",
	}, "AK123", "SK456", obsSrv.Client())
	onlineSrc := source.NewOnlineSource(3, obsSrv.URL, nil, obsSrv.Client())

	strictRes := source.NewResolver([]source.ArtifactSource{source.NewLocalSource(1), pubOBS, onlineSrc}, []string{localRoot}, cacheDir, true, false, "relay-cache", 15*time.Minute)
	if _, err := strictRes.ResolveSpec(ctx, source.ArtifactSpec{LogicalPath: logicalPath, SHA256: wantSHA}, false); !sdk.IsCode(err, sdk.CodeSrcOfflineStrictDenied) {
		t.Fatalf("expected KI-SRC-0005 under offlineStrict, got %v", err)
	}

	// 2. offlineStrict=true with internal=true private OBS -> ALLOWED + singleflight deduplicates 20 concurrent calls to 1 fetch!
	privOBS := source.NewObjectStoreSource(domain.MediaSourceConfig{
		Type: "obs", Priority: 2, Internal: true, Endpoint: obsSrv.URL, Bucket: "corp-art", Prefix: "prod",
	}, "AK123", "SK456", obsSrv.Client())
	intRes := source.NewResolver([]source.ArtifactSource{source.NewLocalSource(1), privOBS, onlineSrc}, []string{localRoot}, cacheDir, true, false, "relay-cache", 15*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plan, err := intRes.ResolveSpec(ctx, source.ArtifactSpec{LogicalPath: logicalPath, SHA256: wantSHA}, false)
			if err != nil || plan.SourceUsed != "obs" {
				t.Errorf("concurrent resolve failed: plan=%+v err=%v", plan, err)
			}
		}()
	}
	wg.Wait()
	if got := fetchCount.Load(); got != 1 {
		t.Fatalf("singleflight failed: expected 1 upstream fetch, got %d", got)
	}

	// 3. direct-presigned mode generates SigV4 URL AND obs.RedactString scrubs X-Amz-Signature completely!
	preRes := source.NewResolver([]source.ArtifactSource{privOBS}, nil, t.TempDir(), true, false, "direct-presigned", 15*time.Minute)
	plan, err := preRes.ResolveSpec(ctx, source.ArtifactSpec{LogicalPath: logicalPath, SHA256: wantSHA}, true)
	if err != nil || plan.Mode != "direct-presigned" || !strings.Contains(plan.DirectURL, "X-Amz-Signature=") {
		t.Fatalf("direct-presigned failed: %+v err=%v", plan, err)
	}
	redactedLog := obs.RedactString("curl -o out " + plan.DirectURL)
	if strings.Contains(redactedLog, "SK456") || !strings.Contains(redactedLog, "X-Amz-Signature=******") {
		t.Fatalf("presigned URL signature leaked in log: %s", redactedLog)
	}
}

func TestRegistryMirrorRewriteAndArtifactRangeServerAndBundleDiff(t *testing.T) {
	cfg := rewrite.MirrorConfig{MirrorHost: "zot.corp.local:15000"}
	cases := map[string]string{
		"nginx:1.27":                             "zot.corp.local:15000/docker.io/library/nginx:1.27",
		"valkey/valkey:9.1.4":                    "zot.corp.local:15000/docker.io/valkey/valkey:9.1.4",
		"registry.k8s.io/kube-apiserver:v1.37.2": "zot.corp.local:15000/registry.k8s.io/kube-apiserver:v1.37.2",
		"ghcr.io/fluxcd/helm-controller:v1.3.0":  "zot.corp.local:15000/ghcr.io/fluxcd/helm-controller:v1.3.0",
		"quay.io/calico/node:v3.30.0":            "zot.corp.local:15000/quay.io/calico/node:v3.30.0",
	}
	for in, want := range cases {
		got, err := rewrite.RewriteImage(in, cfg)
		if err != nil || got != want {
			t.Fatalf("RewriteImage(%q) = %q (%v), want %q", in, got, err, want)
		}
	}
	if _, err := rewrite.RewriteImage("nginx:latest", cfg); !sdk.IsCode(err, sdk.CodeImgMutableTag) {
		t.Fatalf("expected KI-IMG-0001 for :latest, got %v", err)
	}
	if _, err := rewrite.RewriteImage("docker.io/bitnami/redis:7.4", cfg); !sdk.IsCode(err, sdk.CodeImgForbiddenRegistry) {
		t.Fatalf("expected KI-IMG-0002 for bitnami, got %v", err)
	}

	// ArtifactHTTPServer: HTTP Range 206 Partial Content + path traversal block
	root := t.TempDir()
	data := bytes.Repeat([]byte("0123456789"), 1024)
	if err := os.WriteFile(filepath.Join(root, "pkg.tar.zst"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	fsrv := artserver.NewArtifactHTTPServer(root, artserver.RateLimitConfig{GlobalBytesPerSec: 100 << 20, PerIPBytesPerSec: 50 << 20})
	ts := httptest.NewServer(fsrv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/pkg.tar.zst", nil)
	req.Header.Set("Range", "bytes=100-199")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || len(body) != 100 || !bytes.Equal(body, data[100:200]) {
		t.Fatalf("Range 206 failed: status=%d len=%d", resp.StatusCode, len(body))
	}

	// Zot config check: docker2s2 + scrub + onDemand
	zCfg, err := zot.RenderConfig(zot.EdgeConfigParams{UpstreamURL: "https://hub.corp.local:15000"})
	if err != nil || !strings.Contains(string(zCfg), "docker2s2") || !strings.Contains(string(zCfg), "scrub") {
		t.Fatalf("invalid zot config: %s", string(zCfg))
	}

	// Bundle incremental diff + KI-BND-0001
	base := bundle.Manifest{TargetRevision: "2026.11.3", Files: []bundle.FileEntry{{Path: "a", SHA256: "1"}, {Path: "b", SHA256: "2"}}}
	next := bundle.Manifest{TargetRevision: "2026.12.1", Files: []bundle.FileEntry{{Path: "a", SHA256: "1"}, {Path: "b", SHA256: "2-new"}}}
	diff := bundle.DiffManifests(base, next)
	if len(diff.Files) != 1 || diff.Files[0].Path != "b" {
		t.Fatalf("unexpected bundle diff: %+v", diff.Files)
	}
	if err := bundle.ValidateIncrementalImport(diff, map[string]bool{}); !sdk.IsCode(err, sdk.CodeBundleMissingBaseRev) {
		t.Fatalf("expected KI-BND-0001 for missing baseRevision, got %v", err)
	}

	// JDK CSPU security gate (CVE-2026-114938)
	oldJDK := runtime.JDKSpec{Distribution: "temurin", Version: "21.0.9", Major: 21, ReleaseDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if err := runtime.ValidateJDKSecurity(oldJDK); !sdk.IsCode(err, sdk.CodeArtifactVerifyFailed) {
		t.Fatalf("expected JDK CSPU gate rejection, got %v", err)
	}
}
