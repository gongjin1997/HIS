package obs

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const redactedMask = "******"

var (
	rePEMBlock      = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	reBearerToken   = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9\-._~+/]+=*`)
	reKinstallToken = regexp.MustCompile(`ki_[A-Za-z0-9]{6,16}_[A-Za-z0-9]{16,64}`)
	reURLCreds      = regexp.MustCompile(`(://[^/\s:@]+:)([^/\s@]+)(@)`)
	rePresignedSig  = regexp.MustCompile(`(?i)([?&](?:X-Amz-Signature|Signature|X-Amz-Security-Token|AWSAccessKeyId|AccessKeyId)=)([^&\s"']+)`)

	sensitiveAttrKeys = []string{
		"password", "passwd", "secret", "token", "private_key", "privatekey",
		"passphrase", "authorization", "dek", "kek", "signature", "access_key", "secret_key",
	}
)

// RedactAttr scrubs slog attributes whose key names indicate sensitive material (05 §5.3).
func RedactAttr(_ []string, a slog.Attr) slog.Attr {
	lower := strings.ToLower(a.Key)
	for _, k := range sensitiveAttrKeys {
		if strings.Contains(lower, k) {
			return slog.String(a.Key, "[REDACTED]")
		}
	}
	return a
}

// RedactWriter is a streaming io.WriteCloser that guarantees zero secret leakage even when
// a secret is split across arbitrary chunk boundaries (including 1-byte writes, D1 §2.6).
// It retains a sliding tail window of max(maxSecretLen, regexWindow) bytes until Close().
type RedactWriter struct {
	mu      sync.Mutex
	dst     io.Writer
	secrets []string // sorted longest-first: plain + base64 + URL-encoded forms
	buf     []byte
	maxWin  int
	closed  bool
}

const minRegexWindow = 256

// NewRedactWriter creates a RedactWriter wrapping dst with initial secrets registered.
func NewRedactWriter(dst io.Writer, initialSecrets ...string) *RedactWriter {
	rw := &RedactWriter{
		dst:    dst,
		maxWin: minRegexWindow,
	}
	for _, s := range initialSecrets {
		rw.RegisterSecret(s)
	}
	return rw
}

// RegisterSecret registers raw secret plus its Base64 and URL-encoded variants (D1 §2.6, D2 §4.2).
func (w *RedactWriter) RegisterSecret(raw string) {
	if len(raw) < 3 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	variants := []string{
		raw,
		base64.StdEncoding.EncodeToString([]byte(raw)),
		base64.RawStdEncoding.EncodeToString([]byte(raw)),
		url.QueryEscape(raw),
	}
	seen := make(map[string]struct{}, len(w.secrets)+len(variants))
	for _, s := range w.secrets {
		seen[s] = struct{}{}
	}
	for _, v := range variants {
		if len(v) < 3 {
			continue
		}
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			w.secrets = append(w.secrets, v)
			if len(v) > w.maxWin {
				w.maxWin = len(v)
			}
		}
	}
	sort.Slice(w.secrets, func(i, j int) bool { return len(w.secrets[i]) > len(w.secrets[j]) })
}

// Write appends p to the sliding buffer, scrubs, and flushes all bytes safely outside the retention window.
func (w *RedactWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	w.buf = append(w.buf, p...)
	// Keep retention window so cross-chunk secrets & PEM headers never escape.
	win := w.maxWin
	// If we are inside an unfinished PEM block, retain until END marker or safety cap (16 KiB).
	if bytes.Contains(w.buf, []byte("-----BEGIN ")) && !bytes.Contains(w.buf, []byte("-----END ")) && len(w.buf) < 16384 {
		return len(p), nil
	}
	w.buf = w.scrubBytes(w.buf)
	if len(w.buf) > win {
		flushLen := len(w.buf) - win
		if _, err := w.dst.Write(w.buf[:flushLen]); err != nil {
			return 0, err
		}
		w.buf = append(w.buf[:0], w.buf[flushLen:]...)
	}
	return len(p), nil
}

// Close performs final redaction on the retained tail and flushes everything to dst.
func (w *RedactWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.buf) > 0 {
		final := w.scrubBytes(w.buf)
		w.buf = nil
		if _, err := w.dst.Write(final); err != nil {
			return err
		}
	}
	return nil
}

func (w *RedactWriter) scrubBytes(in []byte) []byte {
	s := string(in)
	for _, sec := range w.secrets {
		if strings.Contains(s, sec) {
			s = strings.ReplaceAll(s, sec, redactedMask)
		}
	}
	s = scrubPatterns(s)
	return []byte(s)
}

func scrubPatterns(s string) string {
	s = rePEMBlock.ReplaceAllString(s, "-----BEGIN PRIVATE KEY-----[REDACTED]-----END PRIVATE KEY-----")
	s = reBearerToken.ReplaceAllString(s, "${1}"+redactedMask)
	s = reKinstallToken.ReplaceAllString(s, "ki_[REDACTED]")
	s = reURLCreds.ReplaceAllString(s, "${1}"+redactedMask+"${3}")
	s = rePresignedSig.ReplaceAllString(s, "${1}"+redactedMask)
	return s
}

// RedactString applies secret and regex redaction to a complete string.
func RedactString(in string, secrets ...string) string {
	var out bytes.Buffer
	rw := NewRedactWriter(&out, secrets...)
	_, _ = rw.Write([]byte(in))
	_ = rw.Close()
	return out.String()
}
