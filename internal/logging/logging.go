// Package logging configures the standard library structured logger (log/slog)
// for onclaw. Keeping this in its own package means log level/format can be
// reconfigured at runtime from CLI flags or config without touching call sites.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New builds an *slog.Logger at the given level and format writing to out.
//
//   - level: "debug" | "info" | "warn" | "error" (case-insensitive; "" => info)
//   - format: "text" | "json" (case-insensitive; "" => text)
//
// text is best for an interactive terminal; json suits on-device log files.
func New(level, format string, out io.Writer) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	switch normalize(format) {
	case "", "text":
		handler = slog.NewTextHandler(out, opts)
	case "json":
		handler = slog.NewJSONHandler(out, opts)
	default:
		return nil, fmt.Errorf("unsupported log format %q (want text or json)", format)
	}
	return slog.New(handler), nil
}

func parseLevel(s string) slog.Level {
	switch normalize(s) {
	case "", "info":
		return slog.LevelInfo
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

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
