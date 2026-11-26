package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleOperationEventsSSE implements G1 §3.2 lossless multi-replica SSE:
// 1. Validate auth & existence BEFORE writing 200 OK
// 2. Clear global WriteTimeout via http.NewResponseController(w).SetWriteDeadline(time.Time{})
// 3. Apply 10s per-frame write deadline
// 4. Subscribe BEFORE replaying DB events (zero gap race!)
// 5. Strict monotonic Last-Event-ID deduplication
// 6. Send terminal `event: operation.done` WITHOUT an `id:` line (prevents frontend loop/dedup drop!)
func (s *Server) handleOperationEventsSSE(w http.ResponseWriter, r *http.Request, opID string) {
	if _, err := s.app.GetOperation(r.Context(), ActorFromContext(r.Context()), opID); err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}

	var lastSentSeq int64
	if hdr := r.Header.Get("Last-Event-ID"); hdr != "" {
		if parsed, err := strconv.ParseInt(hdr, 10, 64); err == nil && parsed > 0 {
			lastSentSeq = parsed
		}
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe BEFORE replaying from DB so boundary events between replay and wait are never missed!
	wakeupCh, unsub := s.app.SubscribeOperationWakeup(opID)
	defer unsub()

	sanitizeEvent := func(name string) string {
		return strings.NewReplacer("\r", "", "\n", "").Replace(name)
	}
	writeFrame := func(seq int64, eventName string, data []byte) error {
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
		defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
		evName := sanitizeEvent(eventName)
		switch {
		case data == nil:
			// #nosec G705 -- text/event-stream SSE comment heartbeat
			if _, err := fmt.Fprintf(w, ": %s\n\n", evName); err != nil {
				return err
			}
		case seq > 0:
			// #nosec G705 -- text/event-stream SSE frame with sanitized event name and JSON payload
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", seq, evName, data); err != nil {
				return err
			}
		default:
			// Control frame (e.g. operation.done): explicitly NO `id:` line! (G1 §3.2, ARCHITECTURE §5.5)
			// #nosec G705 -- text/event-stream SSE frame with sanitized event name and JSON payload
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evName, data); err != nil {
				return err
			}
		}
		return rc.Flush()
	}

	replayAndCheckDone := func() (bool, error) {
		for {
			batch, err := s.app.ListOperationEventsAfter(r.Context(), opID, lastSentSeq, 200)
			if err != nil {
				return false, err
			}
			for _, ev := range batch {
				if ev.Seq <= lastSentSeq {
					continue
				}
				if err := writeFrame(ev.Seq, ev.EventType, ev.PayloadJSON); err != nil {
					return false, err
				}
				lastSentSeq = ev.Seq
			}
			if len(batch) < 200 {
				break
			}
		}
		curState, err := s.app.GetOperationState(r.Context(), opID)
		if err != nil {
			return false, err
		}
		if curState.IsTerminal() {
			_ = writeFrame(0, "operation.done", []byte(fmt.Sprintf(`{"opId":%q,"state":%q}`, opID, curState)))
			return true, nil
		}
		return false, nil
	}

	if done, err := replayAndCheckDone(); err != nil || done {
		return
	}

	keepalive := time.NewTicker(15 * time.Second)
	fallbackPoll := time.NewTicker(2 * time.Second)
	defer keepalive.Stop()
	defer fallbackPoll.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.shutdownCtx.Done():
			_ = writeFrame(0, "server-draining-reconnect", nil)
			return
		case <-keepalive.C:
			if err := writeFrame(0, "keepalive", nil); err != nil {
				return
			}
		case <-wakeupCh:
			if done, err := replayAndCheckDone(); err != nil || done {
				return
			}
		case <-fallbackPoll.C:
			if done, err := replayAndCheckDone(); err != nil || done {
				return
			}
		}
	}
}
