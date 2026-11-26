package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

// FileKEKProvider stores a multi-version AES-256 keyring in a strict 0600 file (05 §5.5).
// Line format: "v1:<base64-32B-key>" (highest version encrypts; all versions decrypt).
type FileKEKProvider struct {
	id     string
	mu     sync.RWMutex
	keys   map[int][]byte
	latest int
}

// NewFileKEKProvider loads and validates a 0600 keyring file.
func NewFileKEKProvider(id, path string) (*FileKEKProvider, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, err)
	}
	// Reject group/other readable permissions (05 §5.5)
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false,
			fmt.Errorf("keyring file %s permissions %04o are too broad (must be 0600)", path, fi.Mode().Perm()))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, err)
	}
	keys := make(map[int][]byte)
	latest := 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		verStr, b64, ok := strings.Cut(line, ":")
		if !ok {
			// Single-line raw key defaults to v1
			b64 = line
			verStr = "v1"
		}
		ver, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(verStr), "v"))
		if err != nil || ver <= 0 {
			return nil, fmt.Errorf("invalid keyring version %q", verStr)
		}
		kb, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil || len(kb) != 32 {
			return nil, fmt.Errorf("keyring v%d must be 32-byte base64 key", ver)
		}
		keys[ver] = kb
		if ver > latest {
			latest = ver
		}
	}
	if latest == 0 {
		return nil, fmt.Errorf("empty keyring file %s", path)
	}
	if id == "" {
		id = "local-file-kek"
	}
	return &FileKEKProvider{id: id, keys: keys, latest: latest}, nil
}

// GenerateKeyringFile creates a fresh 0600 keyring file with a random 32-byte v1 KEK.
func GenerateKeyringFile(path string) error {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return err
	}
	line := fmt.Sprintf("v1:%s\n", base64.StdEncoding.EncodeToString(k))
	return os.WriteFile(path, []byte(line), 0o600)
}

// Rotate adds a new random key version to memory (and optionally persists to file).
func (p *FileKEKProvider) Rotate(path string) (int, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return 0, err
	}
	p.mu.Lock()
	nextVer := p.latest + 1
	p.keys[nextVer] = k
	p.latest = nextVer
	if path != "" {
		vers := make([]int, 0, len(p.keys))
		for v := range p.keys {
			vers = append(vers, v)
		}
		sort.Ints(vers)
		var sb strings.Builder
		for _, v := range vers {
			fmt.Fprintf(&sb, "v%d:%s\n", v, base64.StdEncoding.EncodeToString(p.keys[v]))
		}
		_ = os.WriteFile(path, []byte(sb.String()), 0o600)
	}
	p.mu.Unlock()
	return nextVer, nil
}

func (p *FileKEKProvider) ID() string { return p.id }
func (p *FileKEKProvider) Version() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.latest
}

func (p *FileKEKProvider) Wrap(_ context.Context, dek []byte, contextStr string) (string, int, error) {
	p.mu.RLock()
	ver := p.latest
	key := p.keys[ver]
	p.mu.RUnlock()

	blk, err := aes.NewCipher(key)
	if err != nil {
		return "", 0, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return "", 0, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", 0, err
	}
	ct := gcm.Seal(nonce, nonce, dek, []byte(contextStr))
	return fmt.Sprintf("local:v%d:%s", ver, base64.StdEncoding.EncodeToString(ct)), ver, nil
}

func (p *FileKEKProvider) Unwrap(_ context.Context, wrapped string, contextStr string) ([]byte, error) {
	parts := strings.SplitN(wrapped, ":", 3)
	if len(parts) != 3 || parts[0] != "local" {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, fmt.Errorf("malformed wrapped DEK"))
	}
	ver, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	p.mu.RLock()
	key, ok := p.keys[ver]
	p.mu.RUnlock()
	if !ok {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("KEK version v%d not in keyring", ver))
	}
	raw, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, err)
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, fmt.Errorf("wrapped DEK truncated"))
	}
	dek, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(contextStr))
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false, fmt.Errorf("DEK unwrap failed (context mismatch?): %w", err))
	}
	return dek, nil
}

func (p *FileKEKProvider) Rewrap(ctx context.Context, wrapped string, contextStr string) (string, int, error) {
	dek, err := p.Unwrap(ctx, wrapped, contextStr)
	if err != nil {
		return "", 0, err
	}
	defer clear(dek)
	return p.Wrap(ctx, dek, contextStr)
}

// NewEnvKEKProvider reads a 32-byte base64 master key from KINSTALL_MASTER_KEY and unsets the process env var (05 §5.5).
func NewEnvKEKProvider(envVar string) (*FileKEKProvider, error) {
	if envVar == "" {
		envVar = "KINSTALL_MASTER_KEY"
	}
	val := strings.TrimSpace(os.Getenv(envVar))
	if val == "" {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("env %s not set", envVar))
	}
	_ = os.Unsetenv(envVar)
	kb, err := base64.StdEncoding.DecodeString(val)
	if err != nil || len(kb) != 32 {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("env %s must be 32-byte base64", envVar))
	}
	return &FileKEKProvider{id: "env-kek", keys: map[int][]byte{1: kb}, latest: 1}, nil
}

// OpenBaoConfig configures the OpenBao 2.7.x Transit KEK Provider (D1 §3.2).
type OpenBaoConfig struct {
	Address     string // must be https:// unless DevInsecure=true
	Token       string
	Mount       string // default "transit"
	KeyName     string
	DevInsecure bool
	HTTPClient  *http.Client
}

// OpenBaoKEKProvider integrates OpenBao 2.7.2 Transit enforcing derived=true + context
// across encrypt, decrypt, and /rewrap (avoiding Vault/OpenBao #31791 associated_data silent-ignore bug, D1 §3.2).
type OpenBaoKEKProvider struct {
	cfg    OpenBaoConfig
	client *http.Client
	ver    int
}

func NewOpenBaoKEKProvider(ctx context.Context, cfg OpenBaoConfig) (*OpenBaoKEKProvider, error) {
	if !cfg.DevInsecure && !strings.HasPrefix(strings.ToLower(cfg.Address), "https://") {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false,
			fmt.Errorf("OpenBao address must use https:// in production (D1 §3.2)"))
	}
	if cfg.Mount == "" {
		cfg.Mount = "transit"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	p := &OpenBaoKEKProvider{cfg: cfg, client: hc}
	// Mandatory startup self-check: verify key exists AND derived == true! (D1 §3.2)
	if err := p.verifyDerivedKey(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *OpenBaoKEKProvider) verifyDerivedKey(ctx context.Context) error {
	url := fmt.Sprintf("%s/v1/%s/keys/%s", strings.TrimRight(p.cfg.Address, "/"), p.cfg.Mount, p.cfg.KeyName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", p.cfg.Token)
	resp, err := p.client.Do(req)
	if err != nil {
		return sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, true, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, true, fmt.Errorf("OpenBao is sealed"))
	}
	if resp.StatusCode != http.StatusOK {
		return sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false, fmt.Errorf("OpenBao key check status %d", resp.StatusCode))
	}
	var body struct {
		Data struct {
			Derived       bool `json:"derived"`
			LatestVersion int  `json:"latest_version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if !body.Data.Derived {
		return sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, false,
			fmt.Errorf("OpenBao transit key %q MUST have derived=true to prevent silent context ignore (D1 §3.2)", p.cfg.KeyName))
	}
	p.ver = body.Data.LatestVersion
	return nil
}

func (p *OpenBaoKEKProvider) ID() string   { return p.cfg.KeyName }
func (p *OpenBaoKEKProvider) Version() int { return p.ver }

func (p *OpenBaoKEKProvider) doTransit(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	url := fmt.Sprintf("%s/v1/%s/%s/%s", strings.TrimRight(p.cfg.Address, "/"), p.cfg.Mount, action, p.cfg.KeyName)
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", p.cfg.Token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, true, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, sdk.NewError(sdk.CodeSecretKEKUnavailable, "SEC", http.StatusServiceUnavailable, sdk.CodeSecretKEKUnavailable, true, fmt.Errorf("OpenBao is sealed (no plaintext cache fallback)"))
	}
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, sdk.NewError(sdk.CodeSecretDecryptFailed, "SEC", http.StatusInternalServerError, sdk.CodeSecretDecryptFailed, false,
			fmt.Errorf("OpenBao %s failed (%d): %s", action, resp.StatusCode, strings.TrimSpace(string(msg))))
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

func (p *OpenBaoKEKProvider) Wrap(ctx context.Context, dek []byte, contextStr string) (string, int, error) {
	data, err := p.doTransit(ctx, "encrypt", map[string]any{
		"plaintext": base64.StdEncoding.EncodeToString(dek),
		"context":   base64.StdEncoding.EncodeToString([]byte(contextStr)),
	})
	if err != nil {
		return "", 0, err
	}
	ct, _ := data["ciphertext"].(string)
	ver := parseVaultCipherVersion(ct)
	return ct, ver, nil
}

func (p *OpenBaoKEKProvider) Unwrap(ctx context.Context, wrapped string, contextStr string) ([]byte, error) {
	data, err := p.doTransit(ctx, "decrypt", map[string]any{
		"ciphertext": wrapped,
		"context":    base64.StdEncoding.EncodeToString([]byte(contextStr)),
	})
	if err != nil {
		return nil, err
	}
	ptB64, _ := data["plaintext"].(string)
	return base64.StdEncoding.DecodeString(ptB64)
}

func (p *OpenBaoKEKProvider) Rewrap(ctx context.Context, wrapped string, contextStr string) (string, int, error) {
	data, err := p.doTransit(ctx, "rewrap", map[string]any{
		"ciphertext": wrapped,
		"context":    base64.StdEncoding.EncodeToString([]byte(contextStr)),
	})
	if err != nil {
		return "", 0, err
	}
	ct, _ := data["ciphertext"].(string)
	return ct, parseVaultCipherVersion(ct), nil
}

func parseVaultCipherVersion(ct string) int {
	// Format: vault:v3:...
	parts := strings.SplitN(ct, ":", 3)
	if len(parts) >= 2 {
		v, err := strconv.Atoi(strings.TrimPrefix(parts[1], "v"))
		if err == nil {
			return v
		}
	}
	return 1
}
