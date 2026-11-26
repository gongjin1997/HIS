package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kinstall/kinstall/internal/domain"
	"github.com/kinstall/kinstall/internal/ports"
)

const pgChannel = "kinstall_events"

type notifyPulse struct {
	OpID string `json:"op"`
	Seq  int64  `json:"seq"`
}

// Hub implements ports.EventSink supporting both In-Process single-node (SQLite)
// and Multi-Replica PostgreSQL direct-connection LISTEN/NOTIFY with automatic resync (G1 §3.2).
type Hub struct {
	repo      ports.OperationEventRepo
	directDSN string // non-empty in PostgreSQL HA mode (direct connection bypassing PgBouncer tx pool)
	logger    *slog.Logger
	mu        sync.RWMutex
	subs      map[string]map[chan struct{}]struct{}
}

// NewHub builds an event hub backed by OperationEventRepo (single source of truth).
func NewHub(repo ports.OperationEventRepo, directPGDSN string, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		repo:      repo,
		directDSN: directPGDSN,
		logger:    logger,
		subs:      make(map[string]map[chan struct{}]struct{}),
	}
}

// Publish persists the event into operation_events FIRST (atomic monotonic seq), then wakes local subscribers.
// In PG mode, OperationEventRepo.Append also executes pg_notify inside the same DB transaction.
func (h *Hub) Publish(ctx context.Context, ev domain.OperationEvent) (int64, error) {
	seq, err := h.repo.Append(ctx, &ev)
	if err != nil {
		return 0, err
	}
	h.wakeOp(ev.OperationID)
	return seq, nil
}

// SubscribeWakeup registers a non-blocking coalescing (capacity 1) wakeup channel for opID.
func (h *Hub) SubscribeWakeup(opID string) (<-chan struct{}, func()) {
	c := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[opID] == nil {
		h.subs[opID] = make(map[chan struct{}]struct{})
	}
	h.subs[opID][c] = struct{}{}
	h.mu.Unlock()
	return c, func() {
		h.mu.Lock()
		delete(h.subs[opID], c)
		if len(h.subs[opID]) == 0 {
			delete(h.subs, opID)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) wakeOp(opID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[opID] {
		select {
		case ch <- struct{}{}:
		default: // coalesce backpressure
		}
	}
}

// BroadcastAllResync wakes every active SSE client to replay from DB after reconnect/failover (G1 §3.2).
func (h *Hub) BroadcastAllResync() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, set := range h.subs {
		for ch := range set {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// RunListenLoop maintains the dedicated PG LISTEN connection with exponential backoff and resync pulses.
// In SQLite mode (directDSN == ""), it simply waits for ctx.Done().
func (h *Hub) RunListenLoop(ctx context.Context) error {
	if h.directDSN == "" {
		<-ctx.Done()
		return nil
	}
	backoff := time.Second
	for {
		err := h.listenOnce(ctx)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // graceful shutdown on parent context cancellation
		}
		if err != nil {
			h.logger.Warn("pg LISTEN connection dropped, retrying", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
			if backoff < 15*time.Second {
				backoff *= 2
			}
		}
	}
}

func (h *Hub) listenOnce(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, h.directDSN)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "LISTEN "+pgChannel); err != nil {
		return err
	}
	// Immediately resync all active SSE subscribers so zero events are missed across failover!
	h.BroadcastAllResync()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var p notifyPulse
		if json.Unmarshal([]byte(n.Payload), &p) == nil && p.OpID != "" {
			h.wakeOp(p.OpID)
		}
	}
}
