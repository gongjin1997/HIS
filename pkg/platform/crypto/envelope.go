package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/emmansun/gmsm/sm4"
	"golang.org/x/crypto/argon2"
)

const (
	AlgAES256GCM = "aes256gcm"
	AlgSM4GCM    = "sm4gcm"
	EnvelopePref = "v2:"
)

// KeyringFile persists local master encryption keys and Ed25519 signing keys with 0600 permissions.
type KeyringFile struct {
	Version        string `json:"version"`
	DefaultAlg     string `json:"defaultAlg"`
	MasterKeyHex   string `json:"masterKeyHex"`   // #nosec G117 -- local 0600 keyring storage
	Ed25519PrivHex string `json:"ed25519PrivHex"` // #nosec G117 -- local 0600 keyring storage
}

// Codec provides KMS envelope encryption (AES-256-GCM / SM4-GCM) and Ed25519 signing.
type Codec struct {
	keyVersion string
	defaultAlg string
	aesKey     []byte // 32 bytes
	sm4Key     []byte // 16 bytes
	signPriv   ed25519.PrivateKey
	signPub    ed25519.PublicKey
}

// LoadOrCreateKeyring loads the keyring from path or generates a fresh CSPRNG keyring on first boot.
func LoadOrCreateKeyring(path string) (*Codec, error) {
	if raw, err := os.ReadFile(path); err == nil {
		var kf KeyringFile
		if uerr := json.Unmarshal(raw, &kf); uerr != nil {
			return nil, fmt.Errorf("parse keyring %s: %w", path, uerr)
		}
		return newCodecFromKeyring(kf)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read keyring %s: %w", path, err)
	}

	master := make([]byte, 32)
	if _, err := io.ReadFull(crand.Reader, master); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	kf := KeyringFile{
		Version:        "k1",
		DefaultAlg:     AlgAES256GCM,
		MasterKeyHex:   hex.EncodeToString(master),
		Ed25519PrivHex: hex.EncodeToString(priv),
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir keyring dir: %w", err)
	}
	payload, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return nil, fmt.Errorf("write keyring %s: %w", path, err)
	}
	return newCodecFromKeyring(kf)
}

// NewEphemeralCodec builds an in-memory codec (ideal for tests).
func NewEphemeralCodec(defaultAlg string) (*Codec, error) {
	master := make([]byte, 32)
	if _, err := io.ReadFull(crand.Reader, master); err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		return nil, err
	}
	if defaultAlg == "" {
		defaultAlg = AlgAES256GCM
	}
	return newCodecFromKeyring(KeyringFile{
		Version:        "k1",
		DefaultAlg:     defaultAlg,
		MasterKeyHex:   hex.EncodeToString(master),
		Ed25519PrivHex: hex.EncodeToString(priv),
	})
}

func newCodecFromKeyring(kf KeyringFile) (*Codec, error) {
	master, err := hex.DecodeString(strings.TrimSpace(kf.MasterKeyHex))
	if err != nil || len(master) != 32 {
		return nil, errors.New("master key must be 32-byte hex")
	}
	privBytes, err := hex.DecodeString(strings.TrimSpace(kf.Ed25519PrivHex))
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return nil, errors.New("ed25519 private key must be 64-byte hex")
	}
	priv := ed25519.PrivateKey(privBytes)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid ed25519 public key")
	}

	// Derive dedicated 16-byte SM4 subkey via domain-separated SHA-256.
	h := sha256.New()
	h.Write([]byte("kinstall:sm4-subkey:v2:"))
	h.Write(master)
	sm4Key := h.Sum(nil)[:16]

	alg := kf.DefaultAlg
	if alg != AlgAES256GCM && alg != AlgSM4GCM {
		alg = AlgAES256GCM
	}
	ver := kf.Version
	if ver == "" {
		ver = "k1"
	}
	return &Codec{
		keyVersion: ver,
		defaultAlg: alg,
		aesKey:     master,
		sm4Key:     sm4Key,
		signPriv:   priv,
		signPub:    pub,
	}, nil
}

// SigningKey returns a domain-separated 32-byte HMAC key derived from the master key.
func (c *Codec) SigningKey(purpose string) []byte {
	h := sha256.New()
	h.Write([]byte("kinstall:hmac:"))
	h.Write([]byte(purpose))
	h.Write([]byte(":"))
	h.Write(c.aesKey)
	return h.Sum(nil)
}

// IsEncrypted reports whether s uses the v2 envelope format.
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, EnvelopePref)
}

// Encrypt encrypts plaintext with the default algorithm (AES-256-GCM or SM4-GCM).
func (c *Codec) Encrypt(plaintext string) (string, error) {
	return c.EncryptWithAlg(plaintext, c.defaultAlg)
}

// EncryptWithAlg encrypts plaintext using the specified algorithm ("aes256gcm" or "sm4gcm").
func (c *Codec) EncryptWithAlg(plaintext, alg string) (string, error) {
	aead, err := c.newAEAD(alg)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	aad := []byte(fmt.Sprintf("v2:%s:%s", c.keyVersion, alg))
	ct := aead.Seal(nil, nonce, []byte(plaintext), aad)
	return fmt.Sprintf("v2:%s:%s:%s:%s",
		c.keyVersion,
		alg,
		base64.RawURLEncoding.EncodeToString(nonce),
		base64.RawURLEncoding.EncodeToString(ct),
	), nil
}

// Decrypt decrypts a v2 envelope ciphertext.
func (c *Codec) Decrypt(envelope string) (string, error) {
	if !IsEncrypted(envelope) {
		return "", errors.New("ciphertext does not have v2 envelope prefix")
	}
	parts := strings.Split(envelope, ":")
	if len(parts) != 5 || parts[0] != "v2" {
		return "", errors.New("malformed v2 envelope")
	}
	ver, alg := parts[1], parts[2]
	nonce, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	ct, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	aead, err := c.newAEAD(alg)
	if err != nil {
		return "", err
	}
	if len(nonce) != aead.NonceSize() {
		return "", errors.New("invalid nonce size")
	}
	aad := []byte(fmt.Sprintf("v2:%s:%s", ver, alg))
	pt, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return "", errors.New("envelope authentication failed")
	}
	return string(pt), nil
}

func (c *Codec) newAEAD(alg string) (cipher.AEAD, error) {
	switch alg {
	case AlgAES256GCM:
		block, err := aes.NewCipher(c.aesKey)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case AlgSM4GCM:
		block, err := sm4.NewCipher(c.sm4Key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	default:
		return nil, fmt.Errorf("unsupported envelope algorithm %q", alg)
	}
}

// SignBytes creates a base64url Ed25519 signature over payload.
func (c *Codec) SignBytes(payload []byte) string {
	sig := ed25519.Sign(c.signPriv, payload)
	return base64.RawURLEncoding.EncodeToString(sig)
}

// VerifyBytes validates a base64url Ed25519 signature over payload (fail-closed).
func (c *Codec) VerifyBytes(payload []byte, sigBase64 string) bool {
	if strings.TrimSpace(sigBase64) == "" {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigBase64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(c.signPub, payload, sig)
}

// PublicKeyBase64 returns the base64url-encoded Ed25519 public key.
func (c *Codec) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(c.signPub)
}

// HashPassword hashes a user password with Argon2id (t=2, m=64MiB, p=4).
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(crand.Reader, salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=2,p=4$%s$%s",
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword verifies a plaintext password against an Argon2id hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, uint32(len(expected))) // #nosec G115 -- fixed 32-byte key length
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// SHA256Hex returns the lowercase hexadecimal SHA-256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RandomToken returns a URL-safe random token with nBytes of entropy.
func RandomToken(nBytes int) (string, error) {
	if nBytes < 16 {
		nBytes = 16
	}
	b := make([]byte, nBytes)
	if _, err := io.ReadFull(crand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
