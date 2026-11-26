package obs

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// NewLogger constructs a slog.Logger with automatic key-name scrubbing (05 §5.3).
// format supports "json" (default for server) and "text"/"color" (tint, default for CLI).
func NewLogger(format, level string) *slog.Logger {
	return NewLoggerWithWriter(os.Stderr, format, level)
}

// NewLoggerWithWriter builds a structured logger writing to w.
func NewLoggerWithWriter(w io.Writer, format, level string) *slog.Logger {
	lvl := parseLevel(level)
	if strings.EqualFold(format, "json") {
		h := slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       lvl,
			ReplaceAttr: RedactAttr,
		})
		return slog.New(h)
	}
	h := tint.NewTextHandler(w, &tint.Options{
		Level:       lvl,
		TimeFormat:  time.RFC3339,
		ReplaceAttr: RedactAttr,
	})
	return slog.New(h)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
