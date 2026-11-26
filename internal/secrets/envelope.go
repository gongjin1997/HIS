package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"

	"github.com/emmansun/gmsm/sm4"

	"github.com/kinstall/kinstall/internal/ports"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

const (
	AlgAES256GCM = "aes-256-gcm"
	AlgSM4GCM    = "sm4-gcm"
)

// SecretBytes protects in-memory secret bytes from accidental formatting/logging and supports explicit zeroing (D1 §3.3).
type SecretBytes struct {
	buf []byte
}

func NewSecretBytes(b []byte) *SecretBytes {
	cp := make([]byte, len(b))
	copy(cp, b)
	return &SecretBytes{buf: cp}
}

func (s *SecretBytes) Bytes() []byte {
	if s == nil {
		return nil
	}
	return s.buf
}

func (s *SecretBytes) Zero() {
	if s == nil || len(s.buf) == 0 {
		return
	}
	clear(s.buf)
	runtime.KeepAlive(s.buf)
	s.buf = nil
}

func (s SecretBytes) String() string               { return "[REDACTED]" }
func (s SecretBytes) GoString() string             { return "[REDACTED]" }
func (s SecretBytes) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (s SecretBytes) LogValue() slog.Value         { return slog.StringValue("[REDACTED]") }

// EnvelopeV1 is the canonical stored credential payload (D1 §3.1).
type EnvelopeV1 struct {
	V           int    `json:"v"`
	Alg         string `json:"alg"`         // aes-256-gcm | sm4-gcm
	KEKProvider string `json:"kekProvider"` // file | env | openbao | kmip
	KEKID       string `json:"kekId"`
	KEKVersion  int    `json:"kekVersion"`
	WrappedDEK  string `json:"wrappedDek"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
	KeyVersion  int    `json:"keyVersion"`
}

// BuildAAD constructs unambiguous length-prefixed AAD binding ciphertext to (table, recordID, keyVersion) (D1 §3.1).
func BuildAAD(table, recordID string, keyVersion int) []byte {
	return []byte(fmt.Sprintf("kinstall:env:v1|%d:%s|%d:%s|%d",
		len(table), table, len(recordID), recordID, keyVersion))
}

// BuildKEKContext constructs the canonical OpenBao/KEK derivation context string (D1 §3.2).
func BuildKEKContext(table, recordID string) string {
	return fmt.Sprintf("kinstall:dek:%d:%s:%d:%s", len(table), table, len(recordID), recordID)
}

// EnvelopeStore implements ports.SecretStore with per-record random DEKs, length-prefixed AAD,
// AES-256-GCM (default) or pure-Go SM4-GCM (emmansun/gmsm, ADR-061).
type EnvelopeStore struct {
	kek          ports.KEKProvider
	providerName string
	alg          string
}

func NewEnvelopeStore(kek ports.KEKProvider, providerName, alg string) *EnvelopeStore {
	a := strings.ToLower(strings.TrimSpace(alg))
	if a == "" {
		a = AlgAES256GCM
	}
	if providerName == "" {
		providerName = "file"
	}
	return &EnvelopeStore{
		kek:          kek,
		providerName: providerName,
		alg:          a,
	}
}

func (s *EnvelopeStore) Encrypt(ctx context.Context, table, recordID string, keyVersion int, plaintext []byte) ([]byte, error) {
	if s.kek == nil {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("nil KEK provider"))
	}
	dekLen := 32
	if s.alg == AlgSM4GCM {
		dekLen = 16 // SM4 uses 128-bit key
	}
	dek := make([]byte, dekLen)
	defer clear(dek)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, err
	}

	aead, err := newAEAD(s.alg, dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	aad := BuildAAD(table, recordID, keyVersion)
	ct := aead.Seal(nil, nonce, plaintext, aad)

	wrapped, kekVer, err := s.kek.Wrap(ctx, dek, BuildKEKContext(table, recordID))
	if err != nil {
		return nil, err
	}
	env := EnvelopeV1{
		V:           1,
		Alg:         s.alg,
		KEKProvider: s.providerName,
		KEKID:       s.kek.ID(),
		KEKVersion:  kekVer,
		WrappedDEK:  wrapped,
		Nonce:       base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:  base64.StdEncoding.EncodeToString(ct),
		KeyVersion:  keyVersion,
	}
	return json.Marshal(env)
}

func (s *EnvelopeStore) Decrypt(ctx context.Context, table, recordID string, keyVersion int, envelopeJSON []byte) ([]byte, error) {
	if s.kek == nil {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("nil KEK provider"))
	}
	var env EnvelopeV1
	if err := json.Unmarshal(envelopeJSON, &env); err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	dek, err := s.kek.Unwrap(ctx, env.WrappedDEK, BuildKEKContext(table, recordID))
	if err != nil {
		return nil, err
	}
	defer clear(dek)

	aead, err := newAEAD(env.Alg, dek)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	ct, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	aad := BuildAAD(table, recordID, keyVersion)
	plain, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false,
			fmt.Errorf("aead auth tag / AAD mismatch: %w", err))
	}
	return plain, nil
}

// Rewrap rotates only the wrappedDek under the latest KEK version without altering business ciphertext (05 §5.5, O-11).
func (s *EnvelopeStore) Rewrap(ctx context.Context, table, recordID string, envelopeJSON []byte) ([]byte, error) {
	var env EnvelopeV1
	if err := json.Unmarshal(envelopeJSON, &env); err != nil {
		return nil, err
	}
	newWrapped, newVer, err := s.kek.Rewrap(ctx, env.WrappedDEK, BuildKEKContext(table, recordID))
	if err != nil {
		return nil, err
	}
	env.WrappedDEK = newWrapped
	env.KEKVersion = newVer
	env.KEKID = s.kek.ID()
	return json.Marshal(env)
}

func newAEAD(alg string, key []byte) (cipher.AEAD, error) {
	switch strings.ToLower(alg) {
	case AlgAES256GCM:
		blk, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	case AlgSM4GCM:
		blk, err := sm4.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	default:
		return nil, fmt.Errorf("unsupported envelope cipher %q", alg)
	}
}
