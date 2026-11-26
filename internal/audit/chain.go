package audit

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/emmansun/gmsm/sm3"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// ComputeCanonicalHashes hashes ALL 12 business columns of audit_logs (G1 §5.2).
// SHA-256 and SM3 chains are strictly independent (SM3 uses SM3(payload) + prevSM3 to satisfy GM compliance).
func ComputeCanonicalHashes(seq int64, occurredAt time.Time, e domain.AuditEntry, prevSHA, prevSM3 string, enableGM bool) (shaHex, sm3Hex string) {
	ts := occurredAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	fields := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		seq, ts, e.ActorID, e.ActorType, e.SourceIP, e.ProjectID,
		e.Action, e.TargetType, e.TargetID, e.Result, e.ErrCode, e.TraceID,
	)
	payloadSHA := sha256.Sum256([]byte(e.PayloadJSON))
	shaInput := prevSHA + "|" + fields + "|" + hex.EncodeToString(payloadSHA[:])
	s256 := sha256.Sum256([]byte(shaInput))
	shaHex = hex.EncodeToString(s256[:])

	if enableGM {
		if prevSM3 == "" {
			prevSM3 = GenesisHash
		}
		payloadSM3 := sm3.Sum([]byte(e.PayloadJSON))
		gmInput := prevSM3 + "|" + fields + "|" + hex.EncodeToString(payloadSM3[:])
		gmSum := sm3.Sum([]byte(gmInput))
		sm3Hex = hex.EncodeToString(gmSum[:])
	}
	return shaHex, sm3Hex
}

// Recorder appends fork-free chained audit logs using single-row head locking (G1 §5.2).
type Recorder struct {
	repo     ports.AuditRepo
	enableGM bool
}

func NewRecorder(repo ports.AuditRepo, enableGM bool) *Recorder {
	return &Recorder{repo: repo, enableGM: enableGM}
}

func (r *Recorder) Append(ctx context.Context, entry domain.AuditEntry) (int64, error) {
	if entry.PayloadJSON == "" {
		entry.PayloadJSON = "{}"
	}
	return r.repo.AppendChained(ctx, entry, func(seq int64, ts time.Time, prevSHA, prevSM3 string) (string, string) {
		return ComputeCanonicalHashes(seq, ts, entry, prevSHA, prevSM3, r.enableGM)
	})
}

// VerifyResult reports chain integrity verification results (T-M3-08, kinstall audit verify).
type VerifyResult struct {
	Valid        bool   `json:"valid"`
	TotalChecked int64  `json:"totalChecked"`
	LastSeq      int64  `json:"lastSeq"`
	BrokenAtSeq  int64  `json:"brokenAtSeq,omitempty"`
	BrokenReason string `json:"brokenReason,omitempty"`
	VerifiedGM   bool   `json:"verifiedGm"`
}

// VerifyChain replays every audit_logs row in ascending seq order and pinpoints the exact tampered seq (G1 §5.2).
func VerifyChain(ctx context.Context, repo ports.AuditRepo, verifyGM bool) (*VerifyResult, error) {
	prevSHA := GenesisHash
	prevSM3 := GenesisHash
	var expectedSeq int64 = 1
	var total int64

	for {
		batch, err := repo.List(ctx, "", expectedSeq-1, 500)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, rec := range batch {
			if rec.Seq != expectedSeq {
				return &VerifyResult{
					Valid: false, TotalChecked: total, BrokenAtSeq: expectedSeq,
					BrokenReason: fmt.Sprintf("sequence gap: expected seq %d, got %d", expectedSeq, rec.Seq),
					VerifiedGM:   verifyGM,
				}, nil
			}
			if rec.PrevSHA256 != prevSHA {
				return &VerifyResult{
					Valid: false, TotalChecked: total, BrokenAtSeq: rec.Seq,
					BrokenReason: "prev_sha256 link mismatch (chain fork or deletion)",
					VerifiedGM:   verifyGM,
				}, nil
			}
			wantSHA, wantSM3 := ComputeCanonicalHashes(rec.Seq, rec.OccurredAt, rec.Entry, prevSHA, prevSM3, verifyGM)
			if rec.RecordSHA256 != wantSHA {
				return &VerifyResult{
					Valid: false, TotalChecked: total, BrokenAtSeq: rec.Seq,
					BrokenReason: "record_sha256 digest mismatch (tampered row)",
					VerifiedGM:   verifyGM,
				}, nil
			}
			if verifyGM && rec.RecordSM3 != "" {
				if rec.PrevSM3 != prevSM3 || rec.RecordSM3 != wantSM3 {
					return &VerifyResult{
						Valid: false, TotalChecked: total, BrokenAtSeq: rec.Seq,
						BrokenReason: "SM3 chain digest mismatch (tampered row)",
						VerifiedGM:   verifyGM,
					}, nil
				}
				prevSM3 = rec.RecordSM3
			}
			prevSHA = rec.RecordSHA256
			expectedSeq++
			total++
		}
		if len(batch) < 500 {
			break
		}
	}
	return &VerifyResult{Valid: true, TotalChecked: total, LastSeq: expectedSeq - 1, VerifiedGM: verifyGM}, nil
}

// SyslogConfig configures asynchronous RFC 5425 TLS Syslog outbox forwarding (G1 §5.1).
type SyslogConfig struct {
	Enabled   bool
	SinkID    string
	Address   string // host:port
	TLSConfig *tls.Config
	Interval  time.Duration
}

// SyslogForwarder drains audit_logs asynchronously using audit_syslog_cursor so SIEM outages never block writes.
type SyslogForwarder struct {
	repo   ports.AuditRepo
	cfg    SyslogConfig
	logger *slog.Logger
}

func NewSyslogForwarder(repo ports.AuditRepo, cfg SyslogConfig, logger *slog.Logger) *SyslogForwarder {
	if cfg.SinkID == "" {
		cfg.SinkID = "default-siem"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SyslogForwarder{repo: repo, cfg: cfg, logger: logger}
}

func (f *SyslogForwarder) RunLoop(ctx context.Context) error {
	if !f.cfg.Enabled || f.cfg.Address == "" {
		<-ctx.Done()
		return nil
	}
	t := time.NewTicker(f.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = f.FlushOnce(ctx)
		}
	}
}

// FlushOnce forwards pending records via RFC 5425 octet-counted TLS syslog frames and advances cursor.
func (f *SyslogForwarder) FlushOnce(ctx context.Context) error {
	cursor, err := f.repo.GetSyslogCursor(ctx, f.cfg.SinkID)
	if err != nil {
		return err
	}
	batch, err := f.repo.List(ctx, "", cursor, 200)
	if err != nil || len(batch) == 0 {
		return err
	}
	tlsCfg := f.cfg.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	dialer := &tls.Dialer{Config: tlsCfg}
	conn, err := dialer.DialContext(ctx, "tcp", f.cfg.Address)
	if err != nil {
		f.logger.Warn("syslog dial failed; will retry on next tick", slog.String("error", err.Error()))
		return err
	}
	defer func() { _ = conn.Close() }()

	lastSent := cursor
	for _, rec := range batch {
		msg := fmt.Sprintf("<134>1 %s kinstall audit - - - seq=%d actor=%s action=%s target=%s/%s result=%s sha256=%s",
			rec.OccurredAt.UTC().Format(time.RFC3339), rec.Seq, rec.Entry.ActorID, rec.Entry.Action,
			rec.Entry.TargetType, rec.Entry.TargetID, rec.Entry.Result, rec.RecordSHA256)
		// RFC 5425 octet-counting framing: "<len> <msg>"
		frame := fmt.Sprintf("%d %s", len(msg), msg)
		if _, werr := conn.Write([]byte(frame)); werr != nil {
			break
		}
		lastSent = rec.Seq
	}
	if lastSent > cursor {
		return f.repo.UpdateSyslogCursor(ctx, f.cfg.SinkID, lastSent)
	}
	return nil
}
