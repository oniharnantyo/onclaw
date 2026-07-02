package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
)

// Config represents the parameters needed to bootstrap Langfuse tracing.
// It is independent of internal/config to maintain a leaf package status.
type Config struct {
	Host      string
	PublicKey string
	SecretKey string
	SessionID string
	Release   string
	Mask      bool
}

// Setup configures Langfuse tracing if enabled.
// If all credentials are empty, tracing is disabled and it returns (nil, nil).
// If some but not all credentials are set, it returns a validation error.
func Setup(_ context.Context, cfg Config, maskFunc func(string) string) (func(), error) {
	slog.Debug("observability_setup_check",
		"host", cfg.Host,
		"public_key", maskFirstChars(cfg.PublicKey),
		"secret_key", maskFirstChars(cfg.SecretKey),
		"session_id", cfg.SessionID,
		"release", cfg.Release,
		"mask", cfg.Mask,
	)

	// Check if all are empty
	if cfg.Host == "" && cfg.PublicKey == "" && cfg.SecretKey == "" {
		slog.Info("observability_disabled", "reason", "no_credentials")
		return nil, nil
	}

	// Validate configuration completeness
	var missing []string
	if cfg.Host == "" {
		missing = append(missing, "host")
	}
	if cfg.PublicKey == "" {
		missing = append(missing, "public_key")
	}
	if cfg.SecretKey == "" {
		missing = append(missing, "secret_key")
	}
	if len(missing) > 0 {
		slog.Error("observability_validation_failed", "missing", strings.Join(missing, ", "))
		return nil, fmt.Errorf("langfuse configuration is incomplete: missing %s", strings.Join(missing, ", "))
	}

	// Prepare Langfuse configuration
	lfCfg := buildConfig(cfg, maskFunc)

	slog.Info("observability_enabled",
		"host", cfg.Host,
		"mask", cfg.Mask,
	)

	// Create handler and flush function
	handler, flusher := langfuse.NewLangfuseHandler(lfCfg)

	// Register globally on Eino callback bus
	callbacks.AppendGlobalHandlers(handler)

	slog.Info("observability_handler_registered", "handler_type", "langfuse")

	return flusher, nil
}

func maskFirstChars(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}

func buildConfig(cfg Config, maskFunc func(string) string) *langfuse.Config {
	lfCfg := &langfuse.Config{
		Host:      cfg.Host,
		PublicKey: cfg.PublicKey,
		SecretKey: cfg.SecretKey,
		SessionID: cfg.SessionID,
		Release:   cfg.Release,
	}

	if cfg.Mask && maskFunc != nil {
		lfCfg.MaskFunc = maskFunc
	}
	return lfCfg
}
