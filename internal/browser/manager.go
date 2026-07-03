package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// ToolGroupCfg defines the configuration retrieval interface to avoid circular imports.
type ToolGroupCfg interface {
	GetConfig(ctx context.Context, category string) (string, error)
}

// KVStore defines preference store operations to avoid circular imports.
type KVStore interface {
	Set(ctx context.Context, key string, value string) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

// Factory defines the signature for a browser engine factory.
type Factory func(ctx context.Context, cfg Config) (Engine, func() error, error)

var (
	factoriesMu sync.Mutex
	factories   = make(map[string]Factory)
)

// Register registers a browser engine factory.
func Register(name string, factory Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[name] = factory
}

// Manager manages the lifecycle and active state of the browser engine.
type Manager struct {
	mu           sync.Mutex
	cfg          Config
	engine       Engine
	browserCtx   Context
	pages        []Page
	activeIdx    int
	stopLauncher func() error
	started      bool
	scope        string
	kvStore      KVStore
}

// NewManager creates a new browser Manager.
func NewManager() *Manager {
	return &Manager{
		activeIdx: -1,
	}
}

// Start starts the browser engine with configuration retrieved from toolGroupCfg and persists session under scope.
func (m *Manager) Start(ctx context.Context, scope string, toolGroupCfg ToolGroupCfg, kvStore KVStore) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// 1. Load config
	var rawCfg string
	var err error
	if toolGroupCfg != nil {
		rawCfg, err = toolGroupCfg.GetConfig(ctx, "Browser")
	}

	var cfg Config
	if rawCfg != "" {
		if err := json.Unmarshal([]byte(rawCfg), &cfg); err != nil {
			return fmt.Errorf("failed to parse browser config: %w", err)
		}
	} else {
		// Code defaults: Lightpanda by default
		cfg.Engine = "lightpanda"
		cfg.Lightpanda.Port = 9222
		cfg.Headless = true
	}
	m.cfg = cfg

	// 2. Lookup registered factory
	factoriesMu.Lock()
	factory, ok := factories[cfg.Engine]
	factoriesMu.Unlock()
	if !ok {
		return fmt.Errorf("unsupported browser engine %q (make sure the cdp package is registered)", cfg.Engine)
	}

	// 3. Launch engine via factory
	engine, stopLauncher, err := factory(ctx, cfg)
	if err != nil {
		return fmt.Errorf("engine unavailable: %w", err)
	}

	// 4. Connect CDP engine
	if err := engine.Start(ctx); err != nil {
		if stopLauncher != nil {
			_ = stopLauncher()
		}
		return fmt.Errorf("failed to connect engine: %w", err)
	}

	// 5. Create isolated browser context for this scope
	bCtx, err := engine.NewContext(ctx, scope)
	if err != nil {
		_ = engine.Stop(ctx)
		if stopLauncher != nil {
			_ = stopLauncher()
		}
		return fmt.Errorf("failed to create context: %w", err)
	}

	// 6. Restore cookies from KVStore if available
	if kvStore != nil && scope != "" {
		key := fmt.Sprintf("browser.session.%s", scope)
		if val, err := kvStore.Get(ctx, key); err == nil && val != "" {
			var cookies []Cookie
			if err := json.Unmarshal([]byte(val), &cookies); err == nil {
				_ = bCtx.SetCookies(ctx, cookies)
			}
		}
	}

	m.engine = engine
	m.browserCtx = bCtx
	m.stopLauncher = stopLauncher
	m.scope = scope
	m.kvStore = kvStore
	m.started = true
	m.pages = nil
	m.activeIdx = -1

	return nil
}

// Stop stops the browser engine, saves cookies to KVStore, and cleans up all processes.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	// 1. Save cookies to KVStore
	if m.kvStore != nil && m.scope != "" && m.browserCtx != nil {
		key := fmt.Sprintf("browser.session.%s", m.scope)
		if cookies, err := m.browserCtx.Cookies(ctx); err == nil {
			if val, err := json.Marshal(cookies); err == nil {
				_ = m.kvStore.Set(ctx, key, string(val))
			}
		}
	}

	var firstErr error
	if m.browserCtx != nil {
		if err := m.browserCtx.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		m.browserCtx = nil
	}

	if m.engine != nil {
		if err := m.engine.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		m.engine = nil
	}

	if m.stopLauncher != nil {
		if err := m.stopLauncher(); err != nil && firstErr == nil {
			firstErr = err
		}
		m.stopLauncher = nil
	}

	m.pages = nil
	m.activeIdx = -1
	m.started = false
	m.scope = ""
	m.kvStore = nil

	return firstErr
}

// IsStarted returns whether the browser engine is started.
func (m *Manager) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// OpenPage opens a new tab and sets it as the active page.
func (m *Manager) OpenPage(ctx context.Context, scope string, toolGroupCfg ToolGroupCfg, kvStore KVStore) (Page, error) {
	if !m.IsStarted() {
		if err := m.Start(ctx, scope, toolGroupCfg, kvStore); err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.browserCtx.NewPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open page: %w", err)
	}

	m.pages = append(m.pages, page)
	m.activeIdx = len(m.pages) - 1
	return page, nil
}

// ClosePage closes the active page.
func (m *Manager) ClosePage(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || len(m.pages) == 0 || m.activeIdx < 0 || m.activeIdx >= len(m.pages) {
		return errors.New("no active page to close")
	}

	target := m.pages[m.activeIdx]
	_ = target.Close(ctx)

	// Remove from slice
	m.pages = append(m.pages[:m.activeIdx], m.pages[m.activeIdx+1:]...)

	// Readjust active index
	if len(m.pages) == 0 {
		m.activeIdx = -1
	} else if m.activeIdx >= len(m.pages) {
		m.activeIdx = len(m.pages) - 1
	}

	return nil
}

// GetActivePage returns the current active page.
func (m *Manager) GetActivePage() (Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil, errors.New("browser engine is not started")
	}
	if len(m.pages) == 0 || m.activeIdx < 0 || m.activeIdx >= len(m.pages) {
		return nil, errors.New("no active pages open (use browser_open to open a tab)")
	}

	return m.pages[m.activeIdx], nil
}

// SetActivePage sets the page at index as active.
func (m *Manager) SetActivePage(index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return errors.New("browser engine is not started")
	}
	if index < 0 || index >= len(m.pages) {
		return fmt.Errorf("invalid page index: %d (open tabs: %d)", index, len(m.pages))
	}

	m.activeIdx = index
	return nil
}

// PageInfo represents metadata of a page.
type PageInfo struct {
	Index  int    `json:"index"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// ListPages returns metadata of all open pages.
func (m *Manager) ListPages(ctx context.Context) ([]PageInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil, errors.New("browser engine is not started")
	}

	var list []PageInfo
	for i, p := range m.pages {
		u, _ := p.URL(ctx)
		t, _ := p.Title(ctx)
		list = append(list, PageInfo{
			Index:  i,
			URL:    u,
			Title:  t,
			Active: i == m.activeIdx,
		})
	}
	return list, nil
}
