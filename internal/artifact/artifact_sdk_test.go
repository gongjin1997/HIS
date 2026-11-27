package artifact_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kinstall/kinstall/internal/artifact/oci"
	"github.com/kinstall/kinstall/internal/artifact/source"
	"github.com/kinstall/kinstall/internal/artifact/verify"
	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/registry"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestAWSS3V2PutHeadGetPresignNoAWSChunked(t *testing.T) {
	ctx := context.Background()
	var (
		mu            sync.Mutex
		objects       = map[string][]byte{}
		sawAWSChunked bool
		putSeen       bool
		headSeen      bool
		getSeen       bool
	)
	s3Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.Header.Get("Content-Encoding")), "aws-chunked") ||
			strings.Contains(strings.ToLower(r.Header.Get("x-amz-content-sha256")), "streaming-") {
			mu.Lock()
			sawAWSChunked = true
			mu.Unlock()
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			objects[r.URL.Path] = b
			putSeen = true
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			headSeen = true
			if _, ok := objects[r.URL.Path]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodGet:
			getSeen = true
			if b, ok := objects[r.URL.Path]; ok {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(b)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer s3Srv.Close()

	src := source.NewObjectStoreSource(domain.MediaSourceConfig{
		Type:     "obs",
		Priority: 2,
		Internal: true,
		Endpoint: s3Srv.URL,
		Region:   "cn-north-4",
		Bucket:   "kinstall-prod",
		Prefix:   "releases",
	}, "AK-TEST", "SK-TEST", s3Srv.Client())

	payload := []byte("huawei-obs-s3v2-verified-binary")
	if err := src.PutObject(ctx, "binaries/valkey/valkey.tar.zst", bytes.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}
	if !putSeen || sawAWSChunked {
		t.Fatalf("expected PutObject without aws-chunked: putSeen=%v sawAWSChunked=%v", putSeen, sawAWSChunked)
	}
	spec := source.ArtifactSpec{LogicalPath: "binaries/valkey/valkey.tar.zst"}
	ok, err := src.Exists(ctx, spec)
	if err != nil || !ok || !headSeen {
		t.Fatalf("HeadObject failed: ok=%v err=%v headSeen=%v", ok, err, headSeen)
	}
	var out bytes.Buffer
	if err := src.FetchTo(ctx, spec, &out); err != nil || !bytes.Equal(out.Bytes(), payload) || !getSeen {
		t.Fatalf("GetObject failed: err=%v got=%q", err, out.String())
	}
	preURL, label, err := src.PresignGet(ctx, spec, 0)
	if err != nil || !strings.Contains(preURL, "X-Amz-Signature=") || !strings.Contains(label, "PRESIGNED-REDACTED") {
		t.Fatalf("PresignGetAWS failed: url=%q label=%q err=%v", preURL, label, err)
	}
}

func TestSigstoreOfflineTrustedRootDefense(t *testing.T) {
	if _, err := verify.LoadSigstoreTrustedRoot(nil); !sdk.IsCode(err, sdk.CodeArtifactTrustRootFailed) {
		t.Fatalf("expected KI-ART-0002 on empty trusted root, got %v", err)
	}
	if _, err := verify.LoadSigstoreTrustedRoot([]byte("{bad")); !sdk.IsCode(err, sdk.CodeArtifactTrustRootFailed) {
		t.Fatalf("expected KI-ART-0002 on malformed trusted root, got %v", err)
	}
}

func TestORASOCIArtifactCopyAndSignedDeclarativePluginLoad(t *testing.T) {
	ctx := context.Background()
	srcLayout := t.TempDir()
	dstLayout := t.TempDir()

	pluginYAML := []byte(`
apiVersion: kinstall.io/v1alpha1
kind: DeclarativePlugin
meta:
  name: custom-caddy
  version: "1.0.0"
  sdkVersion: v1alpha1
  type: Component
  products:
    - product: caddy
      versions: ">=2.8.0"
      targets: [host-binary]
operations:
  install:
    phases:
      - name: deploy
        tasks:
          - key: run-caddy
            type: cmd.run
            params:
              script: "echo caddy ready"
`)
	sum := sha256.Sum256(pluginYAML)
	wantSHA := hex.EncodeToString(sum[:])

	priv, err := verify.GenerateSM2KeyPair()
	if err != nil {
		t.Fatalf("sm2 key: %v", err)
	}
	sig, err := verify.SignSM2(priv, pluginYAML)
	if err != nil {
		t.Fatalf("sm2 sign: %v", err)
	}

	if _, err := oci.PackAndPushArtifact(ctx, srcLayout, "v1.0.0", oci.ArtifactTypeDeclarativePlugin, map[string][]byte{
		"plugin.yaml":         pluginYAML,
		"plugin.yaml.sm2.sig": sig,
	}); err != nil {
		t.Fatalf("pack oci artifact: %v", err)
	}
	if _, err := oci.CopyBetweenLayouts(ctx, srcLayout, "v1.0.0", dstLayout, "v1.0.0"); err != nil {
		t.Fatalf("oras.Copy failed: %v", err)
	}
	comp, err := registry.LoadDeclarativeFromOCI(ctx, dstLayout, "v1.0.0", wantSHA, &priv.PublicKey)
	if err != nil {
		t.Fatalf("LoadDeclarativeFromOCI failed: %v", err)
	}
	if comp.Meta().Name != "custom-caddy" {
		t.Fatalf("unexpected plugin name: %s", comp.Meta().Name)
	}
}
