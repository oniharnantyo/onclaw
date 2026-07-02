package cdp

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/oniharnantyo/onclaw/internal/browser"
)

// LaunchLightpanda starts a local Lightpanda process if not already running on the port.
func LaunchLightpanda(ctx context.Context, cfg browser.LightpandaConfig) (string, func() error, error) {
	binPath := cfg.BinPath
	if binPath == "" {
		binPath = "lightpanda"
	}
	port := cfg.Port
	if port == 0 {
		port = 9222
	}

	portStr := fmt.Sprintf("%d", port)
	targetURL := fmt.Sprintf("http://127.0.0.1:%s", portStr)

	// 1. Try connecting to an already running instance first to be robust.
	if wsURL, err := resolveWebSocketURL(ctx, targetURL); err == nil {
		// Already running (or managed externally, e.g. docker or background run)
		noopStop := func() error { return nil }
		return wsURL, noopStop, nil
	}

	// 2. Not running, spawn it.
	cmd := exec.Command(binPath, "serve", "--port", portStr)
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("failed to start lightpanda process: %w", err)
	}

	// 3. Poll until the CDP server becomes active.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(10 * time.Second)
	var wsURL string
	var err error
	ready := false

	for !ready {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return "", nil, ctx.Err()
		case <-timeout:
			_ = cmd.Process.Kill()
			return "", nil, fmt.Errorf("timeout waiting for lightpanda serve on port %s to become ready", portStr)
		case <-ticker.C:
			wsURL, err = resolveWebSocketURL(ctx, targetURL)
			if err == nil {
				ready = true
			}
		}
	}

	stop := func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}

	return wsURL, stop, nil
}
