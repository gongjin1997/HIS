package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/websocket"

	"github.com/kinstall/kinstall/internal/app"
	sdk "github.com/kinstall/kinstall/pkg/sdk/v1alpha1"
)

func (s *Server) handleListJobTemplates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.app.ListJobTemplates()})
}

func (s *Server) handleRunBatchJob(w http.ResponseWriter, r *http.Request) {
	var body app.BatchJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeProblem(w, r, s.i18n, sdk.NewError(sdk.CodeAPIInvalidRequest, "API", http.StatusBadRequest, sdk.CodeAPIInvalidRequest, false, err))
		return
	}
	op, err := s.app.RunBatchJob(r.Context(), ActorFromContext(r.Context()), chi.URLParam(r, "projectId"), body)
	if err != nil {
		writeProblem(w, r, s.i18n, err)
		return
	}
	writeJSON(w, http.StatusAccepted, op)
}

type wsTermMessage struct {
	Type string `json:"type"` // input | resize | ping
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// handleHostTerminalWS upgrades an authenticated HTTP request to an RFC 6455 WebSocket bridged
// directly to a real SSH xterm-256color PTY session on the target CMDB host.
func (s *Server) handleHostTerminalWS(w http.ResponseWriter, r *http.Request) {
	actor := ActorFromContext(r.Context())
	projectID := chi.URLParam(r, "projectId")
	hostID := chi.URLParam(r, "hostId")
	cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}

	// Clear HTTP server WriteTimeout so interactive WebSocket stays open indefinitely
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	wsSrv := websocket.Server{
		Handshake: func(_ *websocket.Config, _ *http.Request) error {
			// Origin already gated by SameSite session cookie / authMiddleware
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			defer func() { _ = ws.Close() }()
			startedAt := time.Now()
			pty, host, err := s.app.OpenHostPTY(r.Context(), actor, projectID, hostID, cols, rows)
			if err != nil {
				_ = websocket.Message.Send(ws, "\r\n\x1b[31m[kinstall WebSSH] 连接失败: "+err.Error()+"\x1b[0m\r\n")
				return
			}
			defer func() {
				_ = pty.Close()
				s.app.RecordTerminalClosed(context.Background(), actor, projectID, host.ID, host.Name, time.Since(startedAt))
			}()

			done := make(chan struct{})
			// PTY stdout -> WebSocket
			go func() {
				defer close(done)
				buf := make([]byte, 8192)
				for {
					n, rerr := pty.Read(buf)
					if n > 0 {
						if serr := websocket.Message.Send(ws, string(buf[:n])); serr != nil {
							return
						}
					}
					if rerr != nil {
						if !errors.Is(rerr, io.EOF) {
							_ = websocket.Message.Send(ws, "\r\n\x1b[33m[会话已结束]\x1b[0m\r\n")
						}
						return
					}
				}
			}()

			// WebSocket -> PTY stdin / resize
			for {
				var raw string
				if err := websocket.Message.Receive(ws, &raw); err != nil {
					break
				}
				var msg wsTermMessage
				if jerr := json.Unmarshal([]byte(raw), &msg); jerr == nil && msg.Type != "" {
					switch msg.Type {
					case "resize":
						_ = pty.Resize(msg.Cols, msg.Rows)
					case "input":
						if msg.Data != "" {
							_, _ = pty.Write([]byte(msg.Data))
						}
					case "ping":
						// keepalive
					}
				} else {
					_, _ = pty.Write([]byte(raw))
				}
			}
			_ = pty.Close()
			<-done
		},
	}
	wsSrv.ServeHTTP(w, r)
}
