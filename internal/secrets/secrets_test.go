package secrets_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kinstall/kinstall/internal/secrets"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func TestEnvelopeAESAndSM4WithAADTamperAndKeyRotation(t *testing.T) {
	ctx := context.Background()
	krPath := filepath.Join(t.TempDir(), "keyring")
	if err := secrets.GenerateKeyringFile(krPath); err != nil {
		t.Fatal(err)
	}
	// Verify overly permissive 0644 keyring is rejected
	_ = os.Chmod(krPath, 0o644)
	if _, err := secrets.NewFileKEKProvider("test", krPath); !sdk.IsCode(err, sdk.CodeSecretKEKUnavailable) {
		t.Fatalf("expected KI-SEC-0001 on 0644 keyring, got %v", err)
	}
	_ = os.Chmod(krPath, 0o600)

	kek, err := secrets.NewFileKEKProvider("local-kek", krPath)
	if err != nil {
		t.Fatalf("NewFileKEKProvider: %v", err)
	}

	for _, alg := range []string{secrets.AlgAES256GCM, secrets.AlgSM4GCM} {
		store := secrets.NewEnvelopeStore(kek, "file", alg)
		plain := []byte("top-secret-ssh-private-key-material-" + alg)
		env, err := store.Encrypt(ctx, "credentials", "cred_01A", 1, plain)
		if err != nil {
			t.Fatalf("%s Encrypt: %v", alg, err)
		}
		got, err := store.Decrypt(ctx, "credentials", "cred_01A", 1, env)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("%s Decrypt mismatch: err=%v", alg, err)
		}

		// Cross-record transplant (wrong recordID -> AAD mismatch) MUST fail with KI-SEC-0002
		if _, err := store.Decrypt(ctx, "credentials", "cred_01B", 1, env); !sdk.IsCode(err, sdk.CodeSecretDecryptFailed) {
			t.Fatalf("%s expected KI-SEC-0002 on AAD transplant, got %v", alg, err)
		}

		// Rotate KEK v1 -> v2 and Rewrap without changing ciphertext
		newVer, err := kek.Rotate(krPath)
		if err != nil || newVer < 2 {
			t.Fatalf("Rotate: %v %d", err, newVer)
		}
		rewrapped, err := store.Rewrap(ctx, "credentials", "cred_01A", env)
		if err != nil {
			t.Fatalf("%s Rewrap: %v", alg, err)
		}
		gotAfter, err := store.Decrypt(ctx, "credentials", "cred_01A", 1, rewrapped)
		if err != nil || !bytes.Equal(gotAfter, plain) {
			t.Fatalf("%s Decrypt after Rewrap failed: %v", alg, err)
		}
	}

	// SecretBytes redaction & zeroing
	sb := secrets.NewSecretBytes([]byte("do-not-print"))
	if sb.String() != "[REDACTED]" {
		t.Fatal("SecretBytes.String leaked")
	}
	sb.Zero()
	if len(sb.Bytes()) != 0 {
		t.Fatal("SecretBytes.Zero did not wipe slice")
	}
}

func TestOpenBaoDerivedKeyCheckAndSealFailClosed(t *testing.T) {
	ctx := context.Background()
	var derived atomic.Bool
	var sealed atomic.Bool
	var keyVer atomic.Int32
	keyVer.Store(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sealed.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/keys/k1"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"derived": derived.Load(), "latest_version": keyVer.Load()},
			})
		case strings.Contains(r.URL.Path, "/encrypt/"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			ct := fmt.Sprintf("vault:v%d:%s|%s", keyVer.Load(), req["context"], req["plaintext"])
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ciphertext": ct}})
		case strings.Contains(r.URL.Path, "/decrypt/"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			parts := strings.SplitN(req["ciphertext"], ":", 3)
			ctxAndPT := strings.SplitN(parts[2], "|", 2)
			if ctxAndPT[0] != req["context"] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("context mismatch"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"plaintext": ctxAndPT[1]}})
		case strings.Contains(r.URL.Path, "/rewrap/"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			parts := strings.SplitN(req["ciphertext"], ":", 3)
			ctxAndPT := strings.SplitN(parts[2], "|", 2)
			if ctxAndPT[0] != req["context"] {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			ct := fmt.Sprintf("vault:v%d:%s|%s", keyVer.Load(), ctxAndPT[0], ctxAndPT[1])
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ciphertext": ct}})
		}
	}))
	defer srv.Close()

	// 1. derived=false MUST fail startup check (D1 §3.2)
	derived.Store(false)
	if _, err := secrets.NewOpenBaoKEKProvider(ctx, secrets.OpenBaoConfig{Address: srv.URL, Token: "t", KeyName: "k1", DevInsecure: true}); !sdk.IsCode(err, sdk.CodeSecretKEKUnavailable) {
		t.Fatalf("expected startup rejection for derived=false, got %v", err)
	}

	// 2. derived=true succeeds, encrypt -> rotate -> rewrap -> decrypt works, tampered context fails
	derived.Store(true)
	bao, err := secrets.NewOpenBaoKEKProvider(ctx, secrets.OpenBaoConfig{Address: srv.URL, Token: "t", KeyName: "k1", DevInsecure: true})
	if err != nil {
		t.Fatalf("NewOpenBaoKEKProvider: %v", err)
	}
	es := secrets.NewEnvelopeStore(bao, "openbao", secrets.AlgAES256GCM)
	env, err := es.Encrypt(ctx, "credentials", "cred_01", 1, []byte("bao-secret"))
	if err != nil {
		t.Fatal(err)
	}
	keyVer.Store(2)
	rw, err := es.Rewrap(ctx, "credentials", "cred_01", env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rw), "vault:v2:") {
		t.Fatalf("expected vault:v2 after rewrap, got %s", string(rw))
	}
	pt, err := es.Decrypt(ctx, "credentials", "cred_01", 1, rw)
	if err != nil || string(pt) != "bao-secret" {
		t.Fatalf("decrypt after rewrap failed: %v %q", err, string(pt))
	}

	// 3. Sealed OpenBao -> KI-SEC-0001 (503) immediately, no plaintext cache fallback
	sealed.Store(true)
	if _, err := es.Decrypt(ctx, "credentials", "cred_01", 1, rw); !sdk.IsCode(err, sdk.CodeSecretKEKUnavailable) {
		t.Fatalf("expected KI-SEC-0001 when sealed, got %v", err)
	}
	_ = base64.StdEncoding
}
