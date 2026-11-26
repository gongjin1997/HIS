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

// Dispatcher handles 5 channels (webhook/email/wecom/dingtalk/feishu) with storm folding (T-M3-12).
type Dispatcher struct {
	repo       ports.NotificationRepo
	client     *http.Client
	foldWindow time.Duration
}

func NewDispatcher(repo ports.NotificationRepo, foldWindow time.Duration) *Dispatcher {
	if foldWindow <= 0 {
		foldWindow = 5 * time.Minute
	}
	return &Dispatcher{
		repo:       repo,
		client:     &http.Client{Timeout: 10 * time.Second},
		foldWindow: foldWindow,
	}
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kinstall-Event", msg.EventType)
	req.Header.Set("X-Kinstall-Delivery", deliveryID)
	if secret := cfg["hmacSecret"]; secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(payload)
		req.Header.Set("X-Kinstall-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("channel %s returned HTTP %d", ch.Name, resp.StatusCode)
	}
	return nil
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
