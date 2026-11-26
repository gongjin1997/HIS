package verify

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// TrustedRoot models a minimal offline sigstore trusted_root.json bundle (D3 §1.3).
type TrustedRoot struct {
	MediaType              string   `json:"mediaType"`
	CertificateAuthorities []string `json:"certificateAuthorities,omitempty"`
	TLogs                  []string `json:"tlogs,omitempty"`
}

// ValidateTrustedRoot ensures an offline trusted_root.json is well-formed and non-empty
// (guards against nil-pointer panics like kyverno#18525, T-M1-10).
func ValidateTrustedRoot(raw []byte) (*TrustedRoot, error) {
	if len(raw) == 0 {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false, fmt.Errorf("empty trusted_root.json"))
	}
	var tr TrustedRoot
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false, err)
	}
	if tr.MediaType == "" {
		return nil, sdk.NewError(sdk.CodeArtifactTrustRootFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactTrustRootFailed, false, fmt.Errorf("trusted_root missing mediaType"))
	}
	return &tr, nil
}

// VerifySHA256 validates r's SHA-256 digest against expectedHex.
func VerifySHA256(r io.Reader, expectedHex string) error {
	if len(expectedHex) != 64 {
		return sdk.NewError(sdk.CodeSrcMissingSHA256, "SRC", http.StatusUnprocessableEntity, sdk.CodeSrcMissingSHA256, false, fmt.Errorf("invalid sha256 %q", expectedHex))
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expectedHex) {
		return sdk.NewError(sdk.CodeArtifactVerifyFailed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactVerifyFailed, false,
			fmt.Errorf("sha256 mismatch: got %s, want %s", got, expectedHex))
	}
	return nil
}

// GenerateSM2KeyPair generates a pure-Go SM2 key pair via emmansun/gmsm (ADR-061).
func GenerateSM2KeyPair() (*sm2.PrivateKey, error) {
	return sm2.GenerateKey(rand.Reader)
}

// SignSM2 signs payload using SM2 with SM3 digest (zero network access, D3 §1.3).
func SignSM2(priv *sm2.PrivateKey, payload []byte) ([]byte, error) {
	if priv == nil {
		return nil, sdk.NewError(sdk.CodeArtifactSM2Failed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactSM2Failed, false, fmt.Errorf("nil SM2 private key"))
	}
	return sm2.SignASN1(rand.Reader, priv, payload, sm2.NewSM2SignerOption(true, nil))
}

// VerifySM2 verifies an ASN.1 SM2/SM3 detached signature offline (D3 §1.3, T-M1-10).
func VerifySM2(pub *ecdsa.PublicKey, payload, sig []byte) error {
	if pub == nil || len(sig) == 0 {
		return sdk.NewError(sdk.CodeArtifactSM2Failed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactSM2Failed, false, fmt.Errorf("missing SM2 public key or signature"))
	}
	if !sm2.VerifyASN1WithSM2(pub, nil, payload, sig) {
		return sdk.NewError(sdk.CodeArtifactSM2Failed, "ART", http.StatusUnprocessableEntity, sdk.CodeArtifactSM2Failed, false, fmt.Errorf("SM2 signature verification failed"))
	}
	return nil
}

// SM3Hex computes the pure-Go SM3 digest in lowercase hex.
func SM3Hex(data []byte) string {
	sum := sm3.Sum(data)
	return hex.EncodeToString(sum[:])
}
