package platform_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"github.com/kinstall/kinstall/pkg/platform/audit"
	"github.com/kinstall/kinstall/pkg/platform/auth"
	"github.com/kinstall/kinstall/pkg/platform/crypto"
	"github.com/kinstall/kinstall/pkg/platform/dag"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
	"github.com/kinstall/kinstall/pkg/platform/netguard"
	"github.com/kinstall/kinstall/pkg/platform/safefs"
)

func TestEnvelopeAESAndSM4AndKeyRotation(t *testing.T) {
	dir := t.TempDir()
	kr := filepath.Join(dir, "keyring.json")
	codec, err := crypto.LoadOrCreateKeyring(kr)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyring: %v", err)
	}
	aad := []byte("ten-1:app-1:secret")

	ctAES, err := codec.SealWithAlg([]byte("super-secret-aes"), aad, crypto.AlgAES256GCM, 0)
	if err != nil {
		t.Fatalf("Seal AES: %v", err)
	}
	if !strings.HasPrefix(ctAES, "v2:1:aes256gcm:") {
		t.Fatalf("unexpected AES envelope prefix: %s", ctAES)
	}
	pt, err := codec.Open(ctAES, aad)
	if err != nil || string(pt) != "super-secret-aes" {
		t.Fatalf("Open AES failed: pt=%q err=%v", pt, err)
	}
	if _, err := codec.Open(ctAES, []byte("wrong-aad")); err == nil {
		t.Fatal("expected AAD mismatch failure")
	}

	ctSM4, err := codec.SealWithAlg([]byte("guo-mi-sm4-secret"), aad, crypto.AlgSM4GCM, 0)
	if err != nil {
		t.Fatalf("Seal SM4: %v", err)
	}
	if !strings.HasPrefix(ctSM4, "v2:1:sm4gcm:") {
		t.Fatalf("unexpected SM4 envelope prefix: %s", ctSM4)
	}
	ptSM4, err := codec.Open(ctSM4, aad)
	if err != nil || string(ptSM4) != "guo-mi-sm4-secret" {
		t.Fatalf("Open SM4 failed: pt=%q err=%v", ptSM4, err)
	}

	// Rotate keyring to v2 and ensure v1 ciphertexts still open!
	newVer, err := codec.RotateKey()
	if err != nil || newVer != 2 {
		t.Fatalf("RotateKey failed: ver=%d err=%v", newVer, err)
	}
	ctV2, err := codec.Seal([]byte("v2-secret"), aad)
	if err != nil || !strings.HasPrefix(ctV2, "v2:2:") {
		t.Fatalf("expected v2 key version ciphertext, got %s err=%v", ctV2, err)
	}
	if ptOld, err := codec.Open(ctAES, aad); err != nil || string(ptOld) != "super-secret-aes" {
		t.Fatalf("old v1 ciphertext failed after rotation: %v", err)
	}

	// Ed25519 sign/verify
	sig := codec.SignBytes([]byte("payload"))
	if !codec.VerifyBytes([]byte("payload"), sig) {
		t.Fatal("Ed25519 verification failed")
	}
}

func TestLegacyDecryptAndServerSignAndPasswords(t *testing.T) {
	// Legacy 3-factor XOR + 1M PBKDF2
	f1 := []byte{0x10, 0x81, 0x22}
	f2 := []byte{0x20, 0x01, 0x11}
	f3 := []byte{0x05, 0x00, 0x03}
	var utf8Pwd []byte
	for i := range f1 {
		x := f1[i] ^ f2[i] ^ f3[i]
		if x < 0x80 {
			utf8Pwd = append(utf8Pwd, x)
		} else {
			r := uint32(0xFF00) | uint32(x)
			utf8Pwd = append(utf8Pwd, byte(0xE0|((r>>12)&0x0F)), byte(0x80|((r>>6)&0x3F)), byte(0x80|(r&0x3F)))
		}
	}
	salt := []byte("0123456789abcdef")
	key := pbkdf2.Key(utf8Pwd, salt, 1_000_000, 32, sha256.New)
	blk, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(blk)
	ct := gcm.Seal(nil, salt[:12], []byte("legacy-db-pass"), nil)
	got, err := crypto.LegacyDecryptOnly(f1, f2, f3, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(ct))
	if err != nil || string(got) != "legacy-db-pass" {
		t.Fatalf("LegacyDecryptOnly failed: got=%q err=%v", got, err)
	}

	// ServerSign v2 anti-replay
	now := time.Now().UTC()
	v := crypto.NewServerSignVerifier([]byte("secret-key"), 60*time.Second)
	ts := now.Format("2006") // invalid ts first
	if err := v.Verify(now, "POST", "/x", ts, "n1", "abc", "bad"); err == nil {
		t.Fatal("expected invalid ts rejection")
	}
	tsStr := strings.TrimSpace(time.Unix(now.Unix(), 0).Format("2006"))
	_ = tsStr
	unixStr := now.UTC().Unix()
	tsGood := formatInt64(unixStr)
	sig := v.Sign("POST", "/api/v1/deploy/jobs", tsGood, "nonce-1", "deadbeef")
	if err := v.Verify(now, "POST", "/api/v1/deploy/jobs", tsGood, "nonce-1", "deadbeef", sig); err != nil {
		t.Fatalf("expected valid ServerSign, got %v", err)
	}
	if err := v.Verify(now, "POST", "/api/v1/deploy/jobs", tsGood, "nonce-1", "deadbeef", sig); err == nil {
		t.Fatal("expected replay nonce rejection")
	}

	// Password policy & progressive lockout
	if err := crypto.ValidatePasswordPolicy("alice", "Alice#1234"); err == nil {
		t.Fatal("expected password containing username to fail")
	}
	if err := crypto.ValidatePasswordPolicy("alice", "LiveOps#2026!"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	st3, lu3 := auth.EvaluateProgressiveLockout(3, now)
	if st3 != "LOCKED" || lu3.Sub(now) != 30*time.Second {
		t.Fatalf("expected 30s lock at 3 fails, got %s %v", st3, lu3.Sub(now))
	}
	st6, lu6 := auth.EvaluateProgressiveLockout(6, now)
	if st6 != "LOCKED" || lu6.Sub(now) != 180*time.Second {
		t.Fatalf("expected 180s lock at 6 fails, got %s %v", st6, lu6.Sub(now))
	}
	st9, _ := auth.EvaluateProgressiveLockout(9, now)
	if st9 != "DISABLED" {
		t.Fatalf("expected DISABLED at 9 fails, got %s", st9)
	}
}

func TestNetguardSafeFSAndAuditRedaction(t *testing.T) {
	// Anti-SSRF blocks metadata IP and loopback
	tr := netguard.NewSafeTransport(nil, false)
	cli := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	if _, err := cli.Get("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("expected cloud metadata SSRF block")
	}
	if _, err := cli.Get("http://127.0.0.1:8080/livez"); err == nil {
		t.Fatal("expected loopback SSRF block when private not allowed")
	}

	// SafeFS ZipSlip & Symlink ban
	root := t.TempDir()
	if _, err := safefs.SafeJoin(root, "../../etc/passwd"); err == nil {
		t.Fatal("expected ZipSlip traversal rejection")
	}
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Name: "evil_link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	if _, err := safefs.SafeExtractTar(&tarBuf, root, safefs.DefaultLimits()); err == nil {
		t.Fatal("expected symlink extraction rejection")
	}

	// Audit RedactingHandler & HMAC tamper detection
	var logBuf bytes.Buffer
	logger := slog.New(audit.NewRedactingHandler(slog.NewJSONHandler(&logBuf, nil)))
	logger.Info("login", "username", "admin", "password", "TopSecret#99!", "token", "lo_live_abc")
	logged := logBuf.String()
	if strings.Contains(logged, "TopSecret") || strings.Contains(logged, "lo_live_abc") {
		t.Fatalf("secret leaked into slog output: %s", logged)
	}

	sec := []byte("audit-secret")
	r1 := audit.Record{ID: "1", PrevHash: "GENESIS", Action: "A", OccurredAt: "2026-01-01T00:00:00Z"}
	r1.HMACSignature = audit.ComputeSignature(sec, r1)
	r2 := audit.Record{ID: "2", PrevHash: r1.HMACSignature, Action: "B", OccurredAt: "2026-01-01T00:01:00Z"}
	r2.HMACSignature = audit.ComputeSignature(sec, r2)
	if ok, _, _ := audit.VerifyChain(sec, []audit.Record{r1, r2}); !ok {
		t.Fatal("valid audit chain failed verification")
	}
	r2.Action = "TAMPERED"
	if ok, idx, _ := audit.VerifyChain(sec, []audit.Record{r1, r2}); ok || idx != 1 {
		t.Fatalf("expected tamper detection at idx=1, got ok=%v idx=%d", ok, idx)
	}
}

func TestDAGCompileLegacyAndCyclesAndSSEValidator(t *testing.T) {
	// Legacy chain skips disabled node while preserving order
	plan, err := dag.Compile([]dag.NodeSpec{
		{Key: "step1", TaskOrder: 10},
		{Key: "step2_disabled", TaskOrder: 20, Disabled: true},
		{Key: "step3", TaskOrder: 30},
	}, nil, false)
	if err != nil {
		t.Fatalf("legacy compile failed: %v", err)
	}
	if len(plan.ActiveNodes) != 2 || len(plan.ActiveEdges) != 1 || plan.ActiveEdges[0].From != "step1" || plan.ActiveEdges[0].To != "step3" {
		t.Fatalf("unexpected legacy chain: %+v", plan)
	}

	// Explicit cycle returns concrete CycleError
	_, err = dag.Compile([]dag.NodeSpec{{Key: "A"}, {Key: "B"}, {Key: "C"}}, []dag.EdgeSpec{
		{From: "A", To: "B"}, {From: "B", To: "C"}, {From: "C", To: "A"},
	}, true)
	if err == nil || !strings.Contains(err.Error(), "->") {
		t.Fatalf("expected concrete cycle error, got %v", err)
	}

	// Quick direct validation of U01/U04/U05/U06/U08
	if _, st, code, _ := httpx.ValidateSSECursor("", 1, 5); st != 409 || code != "SNAPSHOT_REQUIRED" {
		t.Fatalf("U01 mismatch: %d %s", st, code)
	}
	if _, st, code, _ := httpx.ValidateSSECursor("-1", 1, 5); st != 400 || code != "INVALID_CURSOR" {
		t.Fatalf("U04 mismatch: %d %s", st, code)
	}
	if _, st, code, _ := httpx.ValidateSSECursor("99999999999999999999999", 1, 5); st != 400 || code != "INVALID_CURSOR" {
		t.Fatalf("U05 mismatch: %d %s", st, code)
	}
	if _, st, code, _ := httpx.ValidateSSECursor("2", 5, 10); st != 410 || code != "CURSOR_RESET_REQUIRED" {
		t.Fatalf("U06 mismatch: %d %s", st, code)
	}
	if _, st, code, _ := httpx.ValidateSSECursor("11", 5, 10); st != 410 || code != "CURSOR_RESET_REQUIRED" {
		t.Fatalf("U08 mismatch: %d %s", st, code)
	}
	_ = context.Background()
}

func formatInt64(n int64) string {
	return time.Unix(n, 0).UTC().Format("") + string(bytes.TrimSpace([]byte(fmtInt(n))))
}

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
