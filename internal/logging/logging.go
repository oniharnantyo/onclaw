// Package logging configures the standard library structured logger (log/slog)
// for onclaw. Keeping this in its own package means log level/format can be
// reconfigured at runtime from CLI flags or config without touching call sites.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)sk-[a-zA-Z0-9_-]{20,}`),
	regexp.MustCompile(`(?i)nvapi-[a-zA-Z0-9_-]{20,}`),
}

func redactSecretPatterns(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// New builds an *slog.Logger at the given level and format writing to out.
//
//   - level: "debug" | "info" | "warn" | "error" (case-insensitive; "" => info)
//   - format: "text" | "json" (case-insensitive; "" => text)
//
// text is best for an interactive terminal; json suits on-device log files.
func New(level, format string, out io.Writer) (*slog.Logger, error) {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			keyLower := strings.ToLower(a.Key)
			if strings.Contains(keyLower, "api_key") || strings.Contains(keyLower, "secret") || strings.Contains(keyLower, "password") || strings.Contains(keyLower, "passphrase") || strings.Contains(keyLower, "token") {
				if a.Value.Kind() == slog.KindString && a.Value.String() != "" {
					return slog.String(a.Key, "***")
				}
			}
			if a.Value.Kind() == slog.KindString {
				val := a.Value.String()
				redacted := redactSecretPatterns(val)
				if redacted != val {
					return slog.String(a.Key, redacted)
				}
			}
			return a
		},
	}

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
