package browser

import (
	"context"
)

// Engine defines the interface for browser engines.
type Engine interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	NewContext(ctx context.Context, scope string) (Context, error)
}

// Context represents an isolated browser context (like an incognito session).
type Context interface {
	Close(ctx context.Context) error
	NewPage(ctx context.Context) (Page, error)
	Pages(ctx context.Context) ([]Page, error)
	Cookies(ctx context.Context) ([]Cookie, error)
	SetCookies(ctx context.Context, cookies []Cookie) error
}

// Page represents a single browser tab.
type Page interface {
	Close(ctx context.Context) error
	Navigate(ctx context.Context, url string) error
	Snapshot(ctx context.Context, opts SnapshotOpts) (*Snapshot, error)
	Screenshot(ctx context.Context, opts ShotOpts) ([]byte, error)
	Act(ctx context.Context, req ActRequest) error
	URL(ctx context.Context) (string, error)
	Title(ctx context.Context) (string, error)
	ConsoleMessages(ctx context.Context) ([]ConsoleMsg, error)
}
