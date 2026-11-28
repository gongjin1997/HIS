package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kinstall/kinstall/internal/usecase/deploy"
	"github.com/kinstall/kinstall/pkg/platform/httpx"
)

func (s *Server) handleListDeployJobs(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.d.Deploy.ListJobs(r.Context(), scope, r.URL.Query().Get("projectId"), limit)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.WriteList(w, items, nil, len(items))
}

func (s *Server) handleSubmitDeployJob(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	idemKey, err := httpx.ParseIdempotencyKey(r, true)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", err.Error(), nil)
		return
	}
	var req deploy.SubmitJobRequest
	reqHash, err := httpx.DecodeStrictJSON(w, r, &req, 2<<20)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error(), nil)
		return
	}
	out, err := s.d.Deploy.SubmitJob(r.Context(), tc, scope, idemKey, reqHash, req)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, out.Snapshot.Job.Version)
	if out.Replayed {
		w.Header().Set("X-Idempotency-Replayed", "true")
	}
	httpx.WriteJSON(w, out.HTTPStatus, out.Snapshot)
}

func (s *Server) handleGetDeployJob(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	includeTimeline := r.URL.Query().Get("timeline") != "false"
	snap, err := s.d.Deploy.GetJobSnapshot(r.Context(), scope, chi.URLParam(r, "jobId"), includeTimeline)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, snap.Job.Version)
	httpx.WriteJSON(w, http.StatusOK, snap)
}

func (s *Server) handleCancelDeployJob(w http.ResponseWriter, r *http.Request) {
	tc, scope := ctxScope(r)
	expectedVer, st, code, err := httpx.ParseIfMatch(r)
	if err != nil {
		httpx.WriteError(w, st, code, err.Error(), nil)
		return
	}
	idemKey, err := httpx.ParseIdempotencyKey(r, false)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error(), nil)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	reqHash := "cancel-empty"
	if r.ContentLength > 0 {
		if h, derr := httpx.DecodeStrictJSON(w, r, &req, 64<<10); derr == nil {
			reqHash = h
		}
	}
	snap, replayed, err := s.d.Deploy.CancelJob(r.Context(), tc, scope, chi.URLParam(r, "jobId"), idemKey, reqHash, expectedVer)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	httpx.SetVersionETag(w, snap.Job.Version)
	if replayed {
		w.Header().Set("X-Idempotency-Replayed", "true")
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

// handleDeployJobSSE implements LiveOps U01-U09 strict cursor SSE timeline streaming:
//   - Requires Last-Event-ID header (or lastEventId query param for browser EventSource)
//   - Missing -> 409 SNAPSHOT_REQUIRED
//   - Invalid / negative / overflow -> 400 INVALID_CURSOR
//   - Outside [minRetainedSeq-1, lastSeq] -> 410 CURSOR_RESET_REQUIRED
//   - Streams monotonic numeric `id: <seq>` events + id-less `: heartbeat` frames.
func (s *Server) handleDeployJobSSE(w http.ResponseWriter, r *http.Request) {
	_, scope := ctxScope(r)
	jobID := chi.URLParam(r, "jobId")

	snap, err := s.d.Deploy.GetJobSnapshot(r.Context(), scope, jobID, false)
	if err != nil {
		writeDomainErr(w, err)
		return
	}

	rawCursor := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if rawCursor == "" {
		rawCursor = strings.TrimSpace(r.URL.Query().Get("lastEventId"))
	}
	cursor, st, code, msg := httpx.ValidateSSECursor(rawCursor, snap.Job.MinRetainedSeq, snap.LastSeq)
	if st != http.StatusOK {
		httpx.WriteError(w, st, code, msg, map[string]any{
			"minRetainedSeq": snap.Job.MinRetainedSeq,
			"lastSeq":        snap.LastSeq,
		})
		return
	}

	sse, err := httpx.NewSSEWriter(w)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "SSE_UNSUPPORTED", err.Error(), nil)
		return
	}

	// Optional test/short-poll close after catch-up when once=true
	onceOnly := r.URL.Query().Get("once") == "true"

	flushPending := func() (bool, error) {
		evts, lerr := s.d.Deploy.ListTimelineAfter(r.Context(), scope, jobID, cursor, 200)
		if lerr != nil {
			return false, lerr
		}
		for _, ev := range evts {
			if serr := sse.SendEvent(ev.Seq, ev.EventType, ev); serr != nil {
				return false, serr
			}
			cursor = ev.Seq
		}
		curSnap, gerr := s.d.Deploy.GetJobSnapshot(r.Context(), scope, jobID, false)
		if gerr == nil && curSnap.Job.State.IsTerminal() && cursor >= curSnap.LastSeq {
			return true, nil
		}
		return false, nil
	}

	if done, err := flushPending(); err != nil || done || onceOnly {
		return
	}

	notifyCh, unsub := s.d.Deploy.Hub().Subscribe(jobID)
	defer unsub()

	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	// Fallback poll ticker (1s) so cross-process liveops-worker updates stream seamlessly over SQLite WAL!
	poll := time.NewTicker(1 * time.Second)
	defer poll.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-notifyCh:
			if done, err := flushPending(); err != nil || done {
				return
			}
		case <-poll.C:
			if done, err := flushPending(); err != nil || done {
				return
			}
		case <-hb.C:
			if err := sse.Heartbeat(); err != nil {
				return
			}
		}
	}
}
