package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/emmansun/gmsm/sm4"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

type Algorithm string

const (
	AlgAES256GCM Algorithm = "aes256gcm"
	AlgSM4GCM    Algorithm = "sm4gcm"
	EnvelopePref           = "v2:"
)

// KeyVersionRecord stores one versioned KEK entry for seamless key rotation.
type KeyVersionRecord struct {
	Version int    `json:"version"`
	KeyHex  string `json:"keyHex"` // #nosec G117 -- 0600 local keyring file
}

// KeyringFile persists versioned encryption keys and Ed25519 signing keys with 0600 permissions.
type KeyringFile struct {
	ActiveVersion  int                `json:"activeVersion"`
	DefaultAlg     Algorithm          `json:"defaultAlg"`
	Keys           []KeyVersionRecord `json:"keys"`
	Ed25519PrivHex string             `json:"ed25519PrivHex"` // #nosec G117 -- 0600 local keyring file
}

// EnvelopeCodec implements LiveOps KMS envelope encryption (AES-256-GCM + SM4-GCM) with key rotation & Ed25519 signing.
type EnvelopeCodec struct {
	mu            sync.RWMutex
	activeVersion int
	defaultAlg    Algorithm
	aesKeys       map[int][]byte // 32-byte AES-256 keys per version
	sm4Keys       map[int][]byte // 16-byte SM4 subkeys derived per version
	signPriv      ed25519.PrivateKey
	signPub       ed25519.PublicKey
}

// LoadOrCreateKeyring loads or initializes a CSPRNG keyring on disk with 0600 permissions.
func LoadOrCreateKeyring(path string) (*EnvelopeCodec, error) {
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
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		return nil, err
	}
	kf := KeyringFile{
		ActiveVersion:  1,
		DefaultAlg:     AlgAES256GCM,
		Keys:           []KeyVersionRecord{{Version: 1, KeyHex: hex.EncodeToString(master)}},
		Ed25519PrivHex: hex.EncodeToString(priv),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, err
	}
	return newCodecFromKeyring(kf)
}

// NewEphemeralCodec creates an in-memory codec for tests.
func NewEphemeralCodec(alg Algorithm) (*EnvelopeCodec, error) {
	master := make([]byte, 32)
	if _, err := io.ReadFull(crand.Reader, master); err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		return nil, err
	}
	if alg == "" {
		alg = AlgAES256GCM
	}
	return newCodecFromKeyring(KeyringFile{
		ActiveVersion:  1,
		DefaultAlg:     alg,
		Keys:           []KeyVersionRecord{{Version: 1, KeyHex: hex.EncodeToString(master)}},
		Ed25519PrivHex: hex.EncodeToString(priv),
	})
}

func newCodecFromKeyring(kf KeyringFile) (*EnvelopeCodec, error) {
	if len(kf.Keys) == 0 {
		return nil, errors.New("keyring must contain at least one key version")
	}
	privBytes, err := hex.DecodeString(strings.TrimSpace(kf.Ed25519PrivHex))
	if err != nil || len(privBytes) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid ed25519 private key in keyring")
	}
	priv := ed25519.PrivateKey(privBytes)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid ed25519 public key")
	}
	c := &EnvelopeCodec{
		activeVersion: kf.ActiveVersion,
		defaultAlg:    kf.DefaultAlg,
		aesKeys:       make(map[int][]byte, len(kf.Keys)),
		sm4Keys:       make(map[int][]byte, len(kf.Keys)),
		signPriv:      priv,
		signPub:       pub,
	}
	if c.defaultAlg != AlgAES256GCM && c.defaultAlg != AlgSM4GCM {
		c.defaultAlg = AlgAES256GCM
	}
	for _, kv := range kf.Keys {
		raw, derr := hex.DecodeString(strings.TrimSpace(kv.KeyHex))
		if derr != nil || len(raw) != 32 {
			return nil, fmt.Errorf("key version %d must be 32-byte hex", kv.Version)
		}
		c.aesKeys[kv.Version] = raw
		c.sm4Keys[kv.Version] = deriveSM4Subkey(raw)
	}
	if _, ok := c.aesKeys[c.activeVersion]; !ok {
		return nil, fmt.Errorf("active key version %d not found in keyring", c.activeVersion)
	}
	return c, nil
}

func deriveSM4Subkey(aesKey []byte) []byte {
	h := sha256.New()
	h.Write([]byte("liveops:sm4-subkey:v2:"))
	h.Write(aesKey)
	return h.Sum(nil)[:16]
}

// ActiveVersion returns the currently active KEK version.
func (c *EnvelopeCodec) ActiveVersion() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeVersion
}

// RotateKey adds a fresh CSPRNG key version while keeping older versions decryptable.
func (c *EnvelopeCodec) RotateKey() (int, error) {
	nextKey := make([]byte, 32)
	if _, err := io.ReadFull(crand.Reader, nextKey); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	nextVer := c.activeVersion + 1
	c.aesKeys[nextVer] = nextKey
	c.sm4Keys[nextVer] = deriveSM4Subkey(nextKey)
	c.activeVersion = nextVer
	return nextVer, nil
}

// DeriveHMACKey returns a domain-separated 32-byte HMAC secret from key version 1.
func (c *EnvelopeCodec) DeriveHMACKey(purpose string) []byte {
	c.mu.RLock()
	base := c.aesKeys[1]
	if len(base) == 0 {
		base = c.aesKeys[c.activeVersion]
	}
	c.mu.RUnlock()
	h := sha256.New()
	h.Write([]byte("liveops:hmac:" + purpose + ":"))
	h.Write(base)
	return h.Sum(nil)
}

// Seal produces v2:{keyVer}:{alg}:{base64(nonce)}:{base64(ciphertext+tag)} (LiveOps 04.2 Spec).
func (c *EnvelopeCodec) Seal(plaintext, aad []byte) (string, error) {
	c.mu.RLock()
	ver, alg := c.activeVersion, c.defaultAlg
	c.mu.RUnlock()
	return c.SealWithAlg(plaintext, aad, alg, ver)
}

// SealWithAlg seals plaintext with an explicit algorithm ("aes256gcm" or "sm4gcm").
func (c *EnvelopeCodec) SealWithAlg(plaintext, aad []byte, alg Algorithm, ver int) (string, error) {
	c.mu.RLock()
	if ver <= 0 {
		ver = c.activeVersion
	}
	aead, err := c.newAEADLocked(ver, alg)
	c.mu.RUnlock()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil {
		return "", err
	}
	fullAAD := buildEnvelopeAAD(ver, alg, aad)
	ct := aead.Seal(nil, nonce, plaintext, fullAAD)
	return fmt.Sprintf("v2:%d:%s:%s:%s",
		ver, alg,
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(ct),
	), nil
}

// Open verifies and decrypts a v2 envelope ciphertext across any retained key version.
func (c *EnvelopeCodec) Open(encoded string, aad []byte) ([]byte, error) {
	parts := strings.Split(encoded, ":")
	if len(parts) != 5 || parts[0] != "v2" {
		return nil, errors.New("invalid v2 ciphertext format")
	}
	ver, err := strconv.Atoi(parts[1])
	if err != nil || ver <= 0 {
		return nil, errors.New("invalid key version in v2 envelope")
	}
	alg := Algorithm(parts[2])
	nonce, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, err
	}
	c.mu.RLock()
	aead, err := c.newAEADLocked(ver, alg)
	c.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	return aead.Open(nil, nonce, ct, buildEnvelopeAAD(ver, alg, aad))
}

func buildEnvelopeAAD(ver int, alg Algorithm, aad []byte) []byte {
	prefix := fmt.Sprintf("v2:%d:%s|", ver, alg)
	out := make([]byte, 0, len(prefix)+len(aad))
	out = append(out, prefix...)
	out = append(out, aad...)
	return out
}

func (c *EnvelopeCodec) newAEADLocked(ver int, alg Algorithm) (cipher.AEAD, error) {
	switch alg {
	case AlgAES256GCM:
		k, ok := c.aesKeys[ver]
		if !ok {
			return nil, fmt.Errorf("aes key version %d not found", ver)
		}
		blk, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	case AlgSM4GCM:
		k, ok := c.sm4Keys[ver]
		if !ok {
			return nil, fmt.Errorf("sm4 key version %d not found", ver)
		}
		blk, err := sm4.NewCipher(k)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(blk)
	default:
		return nil, fmt.Errorf("unsupported envelope algorithm %q", alg)
	}
}

// SignBytes creates an Ed25519 signature over payload.
func (c *EnvelopeCodec) SignBytes(payload []byte) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(c.signPriv, payload))
}

// VerifyBytes performs fail-closed Ed25519 signature verification.
func (c *EnvelopeCodec) VerifyBytes(payload []byte, sigBase64 string) bool {
	if strings.TrimSpace(sigBase64) == "" {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigBase64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(c.signPub, payload, sig)
}

// PublicKeyBase64 returns the Ed25519 public key in base64url form.
func (c *EnvelopeCodec) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(c.signPub)
}

// LegacyDecryptOnly decodes historical LiveOps 3-factor signed-int8 XOR + 1,000,000 PBKDF2-HMAC-SHA256 ciphertexts
// strictly for offline migration via liveopsctl (never used for new writes).
func LegacyDecryptOnly(f1, f2, f3 []byte, saltB64, cipherB64 string) ([]byte, error) {
	if len(f1) == 0 || len(f1) != len(f2) || len(f2) != len(f3) {
		return nil, errors.New("legacy factors length mismatch")
	}
	var utf8Pwd []byte
	for i := range f1 {
		x := f1[i] ^ f2[i] ^ f3[i]
		if x < 0x80 {
			utf8Pwd = append(utf8Pwd, x)
		} else {
			// Reproduce Java signed-byte (int8 -> char 0xFF80..0xFFFF) 3-byte UTF-8 encoding safely.
			r := uint32(0xFF00) | uint32(x)
			utf8Pwd = append(utf8Pwd,
				byte(0xE0|((r>>12)&0x0F)), // #nosec G115 -- bounded to 0xEF
				byte(0x80|((r>>6)&0x3F)),  // #nosec G115 -- bounded to 0xBF
				byte(0x80|(r&0x3F)),       // #nosec G115 -- bounded to 0xBF
			)
		}
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil || len(salt) < 12 {
		return nil, errors.New("invalid legacy salt")
	}
	rawCipher, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return nil, err
	}
	key := pbkdf2.Key(utf8Pwd, salt, 1_000_000, 32, sha256.New)
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, salt[:12], rawCipher, nil)
}

// ServerSignVerifier implements ServerSign v2 anti-replay HMAC-SHA256 verification (±60s skew + nonce dedup).
type ServerSignVerifier struct {
	secret []byte
	skew   time.Duration
	mu     sync.Mutex
	nonces map[string]time.Time
}

func NewServerSignVerifier(secret []byte, skew time.Duration) *ServerSignVerifier {
	if skew <= 0 {
		skew = 60 * time.Second
	}
	return &ServerSignVerifier{
		secret: append([]byte(nil), secret...),
		skew:   skew,
		nonces: make(map[string]time.Time),
	}
}

func (v *ServerSignVerifier) Sign(method, path, tsUnix, nonce, bodySHA256 string) string {
	mac := hmac.New(sha256.New, v.secret)
	mac.Write([]byte(method + "\n" + path + "\n" + tsUnix + "\n" + nonce + "\n" + bodySHA256))
	return hex.EncodeToString(mac.Sum(nil))
}

func (v *ServerSignVerifier) Verify(now time.Time, method, path, tsUnix, nonce, bodySHA256, providedSig string) error {
	ts, err := strconv.ParseInt(tsUnix, 10, 64)
	if err != nil || strings.TrimSpace(nonce) == "" {
		return errors.New("missing timestamp or nonce")
	}
	if math.Abs(float64(now.UTC().Unix()-ts)) > v.skew.Seconds() {
		return errors.New("signature timestamp expired")
	}
	expected := v.Sign(method, path, tsUnix, nonce, bodySHA256)
	if !hmac.Equal([]byte(expected), []byte(providedSig)) {
		return errors.New("invalid hmac signature")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	// Sweep expired nonces opportunistically.
	for k, exp := range v.nonces {
		if now.After(exp) {
			delete(v.nonces, k)
		}
	}
	if _, seen := v.nonces[nonce]; seen {
		return errors.New("replay nonce detected")
	}
	v.nonces[nonce] = now.Add(2 * v.skew)
	return nil
}

// ValidatePasswordPolicy enforces LiveOps password baseline (8-32 chars, 4 classes, no username/reversed).
func ValidatePasswordPolicy(username, password string) error {
	if len(password) < 8 || len(password) > 32 {
		return errors.New("密码长度必须为 8 至 32 位")
	}
	var upper, lower, digit, special bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		default:
			special = true
		}
	}
	if !upper || !lower || !digit || !special {
		return errors.New("密码必须同时包含大写字母、小写字母、数字和特殊字符")
	}
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" && len(u) >= 3 {
		low := strings.ToLower(password)
		if strings.Contains(low, u) || strings.Contains(low, reverseString(u)) {
			return errors.New("密码不得包含用户名或其倒序")
		}
	}
	return nil
}

func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// HashPassword computes an Argon2id password digest (m=64MiB, t=2, p=4).
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

// VerifyPassword performs constant-time Argon2id verification.
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
	actual := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, uint32(len(expected))) // #nosec G115 -- fixed 32-byte key
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// SHA256Hex returns lowercase hex SHA-256 of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RandomToken returns a URL-safe CSPRNG token with nBytes entropy.
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

// GenerateAppKey generates a LiveOps 256-bit AppKey ("lo_live_<prefix8>_<secret>") plus its SHA-256 hash.
func GenerateAppKey() (plaintext, prefix, sha256Hex string, err error) {
	raw, err := RandomToken(32)
	if err != nil {
		return "", "", "", err
	}
	prefix = raw[:8]
	plaintext = "lo_live_" + prefix + "_" + raw[8:]
	sha256Hex = SHA256Hex([]byte(plaintext))
	return plaintext, prefix, sha256Hex, nil
}
