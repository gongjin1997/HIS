package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
)

// EventMessage describes an outbound alert/event notification (05 §4.6, G2 §3).
type EventMessage struct {
	EventType  string         `json:"eventType"`
	ProjectID  string         `json:"projectId"`
	Severity   string         `json:"severity"` // info | warn | critical
	DedupKey   string         `json:"dedupKey"`
	Summary    string         `json:"summary"`
	Details    map[string]any `json:"details,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

// SecretResolver resolves a Vault credential ID to plaintext webhook signing key bytes.
type SecretResolver func(ctx context.Context, credID string) ([]byte, error)

// Dispatcher handles 5 channels (webhook/email/wecom/dingtalk/feishu) with storm folding,
// Vault HMAC secret resolution, and exponential-backoff retries on transient 5xx/network failures (T-M3-12).
type Dispatcher struct {
	repo           ports.NotificationRepo
	client         *http.Client
	foldWindow     time.Duration
	secretResolver SecretResolver
	maxAttempts    int
	baseBackoff    time.Duration
}

func NewDispatcher(repo ports.NotificationRepo, foldWindow time.Duration) *Dispatcher {
	if foldWindow <= 0 {
		foldWindow = 5 * time.Minute
	}
	return &Dispatcher{
		repo:        repo,
		client:      &http.Client{Timeout: 10 * time.Second},
		foldWindow:  foldWindow,
		maxAttempts: 3,
		baseBackoff: 200 * time.Millisecond,
	}
}

// WithSecretResolver configures Vault credential decryption for NotificationChannel.CredentialRef.
func (d *Dispatcher) WithSecretResolver(fn SecretResolver) *Dispatcher {
	d.secretResolver = fn
	return d
}

// WithRetryPolicy overrides default retry attempts and base backoff (useful for tests).
func (d *Dispatcher) WithRetryPolicy(attempts int, baseBackoff time.Duration) *Dispatcher {
	if attempts > 0 {
		d.maxAttempts = attempts
	}
	if baseBackoff >= 0 {
		d.baseBackoff = baseBackoff
	}
	return d
}

// Dispatch evaluates enabled channels for msg.ProjectID, folds storms within foldWindow,
// and delivers non-folded alerts (T-M3-12: 100 identical alerts in 1 min -> 1 delivery!).
func (d *Dispatcher) Dispatch(ctx context.Context, msg EventMessage) (sent int, folded int, err error) {
	channels, err := d.repo.ListChannels(ctx, msg.ProjectID)
	if err != nil {
		return 0, 0, err
	}
	dedup := msg.DedupKey
	if dedup == "" {
		dedup = msg.ProjectID + ":" + msg.EventType
	}
	for _, ch := range channels {
		if !ch.Enabled || !matchesEvent(ch.Events, msg.EventType) {
			continue
		}
		deliv := &domain.NotificationDelivery{
			ChannelID:   ch.ID,
			DedupKey:    dedup,
			WindowEndAt: time.Now().UTC().Add(d.foldWindow),
		}
		isFolded, uerr := d.repo.UpsertDelivery(ctx, deliv)
		if uerr != nil {
			continue
		}
		if isFolded {
			folded++
			continue
		}
		if derr := d.deliverChannel(ctx, ch, deliv.ID, msg); derr != nil {
			_ = d.repo.MarkDelivery(ctx, deliv.ID, "failed", derr.Error())
		} else {
			_ = d.repo.MarkDelivery(ctx, deliv.ID, "sent", "")
			sent++
		}
	}
	return sent, folded, nil
}

func (d *Dispatcher) deliverChannel(ctx context.Context, ch domain.NotificationChannel, deliveryID string, msg EventMessage) error {
	var cfg map[string]string
	_ = json.Unmarshal(ch.ConfigJSON, &cfg)
	endpoint := cfg["url"]
	if endpoint == "" {
		return nil
	}
	var payload []byte
	switch ch.Kind {
	case "wecom":
		payload, _ = json.Marshal(map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": fmt.Sprintf("**[kinstall %s]** %s", msg.Severity, msg.Summary)}})
	case "dingtalk":
		payload, _ = json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": fmt.Sprintf("[kinstall %s] %s", msg.Severity, msg.Summary)}})
	case "feishu":
		payload, _ = json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": fmt.Sprintf("[kinstall %s] %s", msg.Severity, msg.Summary)}})
	default:
		payload, _ = json.Marshal(msg)
	}
	var secretBytes []byte
	if ch.CredentialRef != nil && *ch.CredentialRef != "" && d.secretResolver != nil {
		if raw, serr := d.secretResolver(ctx, *ch.CredentialRef); serr == nil && len(raw) > 0 {
			secretBytes = raw
			defer clear(secretBytes)
		}
	}
	if len(secretBytes) == 0 && cfg["hmacSecret"] != "" {
		secretBytes = []byte(cfg["hmacSecret"])
	}
	sigHeader := ""
	if len(secretBytes) > 0 {
		mac := hmac.New(sha256.New, secretBytes)
		_, _ = mac.Write(payload)
		sigHeader = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}

	attempts := d.maxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kinstall-Event", msg.EventType)
		req.Header.Set("X-Kinstall-Delivery", deliveryID)
		if sigHeader != "" {
			req.Header.Set("X-Kinstall-Signature-256", sigHeader)
		}
		resp, err := d.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("channel %s returned HTTP %d", ch.Name, resp.StatusCode)
			// Only retry 429 or 5xx transient server errors; 4xx client errors are permanent
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		} else {
			lastErr = err
		}
		if attempt+1 < attempts {
			wait := d.baseBackoff * time.Duration(1<<attempt)
			if wait > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
		}
	}
	return lastErr
}

func matchesEvent(allowed []string, ev string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == "*" || a == ev {
			return true
		}
	}
	return false
}
