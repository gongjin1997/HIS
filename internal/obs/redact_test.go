package obs_test

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/kinstall/kinstall/internal/obs"
)

func TestRedactWriterSingleByteChunks(t *testing.T) {
	secret := "S3cr3t@Pass#2026/val"
	b64 := base64.StdEncoding.EncodeToString([]byte(secret))
	urlEsc := url.QueryEscape(secret)
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\nABCD1234EFGH5678\n-----END OPENSSH PRIVATE KEY-----"
	presigned := "https://obs.cn-north-4.myhuaweicloud.com/bucket/valkey.tar.gz?X-Amz-Signature=deadbeef1234567890&AWSAccessKeyId=AKIAEXAMPLEKEY"
	payload := "plain=" + secret + " b64=" + b64 + " url=" + urlEsc +
		" hdr=Bearer eyJhbGciOiJSUzI1NiJ9.payload.sig tok=ki_12345678_abcdefghijklmnopqrstuvwxyz123456 " +
		"dsn=postgres://app:MyDbPass99@10.0.0.5:5432/his " + presigned + "\n" + pem + "\nDONE"

	for _, chunkSize := range []int{1, 3, 7, 19, len(payload)} {
		var out bytes.Buffer
		rw := obs.NewRedactWriter(&out, secret)
		for i := 0; i < len(payload); i += chunkSize {
			end := i + chunkSize
			if end > len(payload) {
				end = len(payload)
			}
			if _, err := rw.Write([]byte(payload[i:end])); err != nil {
				t.Fatalf("chunk=%d write: %v", chunkSize, err)
			}
		}
		if err := rw.Close(); err != nil {
			t.Fatalf("chunk=%d close: %v", chunkSize, err)
		}
		got := out.String()
		for _, forbidden := range []string{secret, b64, urlEsc, "ABCD1234EFGH5678", "MyDbPass99", "deadbeef1234567890", "AKIAEXAMPLEKEY", "eyJhbGciOiJSUzI1NiJ9", "abcdefghijklmnopqrstuvwxyz123456"} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("chunk=%d LEAKED %q in output:\n%s", chunkSize, forbidden, got)
			}
		}
		if !strings.HasSuffix(got, "DONE") {
			t.Fatalf("chunk=%d truncated tail: %q", chunkSize, got)
		}
	}
}

func FuzzRedactWriter(f *testing.F) {
	f.Add("SuperSecretPass!@#", "prefix SuperSecretPass!@# suffix", 1)
	f.Add("tok_val_998877", "before tok_val_998877 after", 3)
	f.Fuzz(func(t *testing.T, secret, text string, step int) {
		if len(secret) < 6 {
			return
		}
		if step <= 0 || step > 64 {
			step = 1
		}
		full := text + " " + secret + " " + url.QueryEscape(secret)
		var out bytes.Buffer
		rw := obs.NewRedactWriter(&out, secret)
		for i := 0; i < len(full); i += step {
			end := i + step
			if end > len(full) {
				end = len(full)
			}
			_, _ = rw.Write([]byte(full[i:end]))
		}
		_ = rw.Close()
		if strings.Contains(out.String(), secret) {
			t.Fatalf("secret leaked in fuzz output")
		}
	})
}
