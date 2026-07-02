package cdp

import (
	"context"
	"fmt"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/oniharnantyo/onclaw/internal/browser"
)

// LaunchChromium starts a local Chromium browser using go-rod's launcher.
func LaunchChromium(ctx context.Context, cfg browser.ChromiumConfig, headless bool) (string, func() error, error) {
	l := launcher.New()
	if cfg.BinPath != "" {
		l.Bin(cfg.BinPath)
	}
	l.Headless(headless)

	// Since we are running on limited memory/devices, add robust flags
	l.Set("no-sandbox")
	l.Set("disable-setuid-sandbox")
	l.Set("disable-dev-shm-usage")
	l.Set("disable-gpu")

	wsURL, err := l.Launch()
	if err != nil {
		return "", nil, fmt.Errorf("failed to launch chromium: %w", err)
	}

	stop := func() error {
		l.Kill()
		return nil
	}

	return wsURL, stop, nil
}
