package cdp

import (
	"context"

	"github.com/oniharnantyo/onclaw/internal/browser"
)

func init() {
	// Register Lightpanda
	browser.Register("lightpanda", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		wsURL, stop, err := LaunchLightpanda(ctx, cfg.Lightpanda)
		if err != nil {
			return nil, nil, err
		}
		return NewEngine(wsURL), stop, nil
	})

	// Register Chromium
	browser.Register("chromium", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		wsURL, stop, err := LaunchChromium(ctx, cfg.Chromium, cfg.Headless)
		if err != nil {
			return nil, nil, err
		}
		return NewEngine(wsURL), stop, nil
	})

	// Register Remote
	browser.Register("remote", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		wsURL, stop, err := LaunchRemote(ctx, cfg.Remote)
		if err != nil {
			return nil, nil, err
		}
		return NewEngine(wsURL), stop, nil
	})
}
