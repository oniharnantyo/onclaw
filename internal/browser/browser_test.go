package browser_test

import (
	"context"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/browser"
)

// mockPage implements browser.Page for unit testing.
type mockPage struct {
	navigatedURL string
	closed       bool
	snapshotRes  *browser.Snapshot
	acts         []browser.ActRequest
}

func (m *mockPage) Close(ctx context.Context) error {
	m.closed = true
	return nil
}

func (m *mockPage) Navigate(ctx context.Context, url string) error {
	m.navigatedURL = url
	return nil
}

func (m *mockPage) URL(ctx context.Context) (string, error) {
	return m.navigatedURL, nil
}

func (m *mockPage) Title(ctx context.Context) (string, error) {
	return "Mock Page", nil
}

func (m *mockPage) Snapshot(ctx context.Context, opts browser.SnapshotOpts) (*browser.Snapshot, error) {
	return m.snapshotRes, nil
}

func (m *mockPage) Screenshot(ctx context.Context, opts browser.ShotOpts) ([]byte, error) {
	return []byte("fake png screenshot data"), nil
}

func (m *mockPage) Act(ctx context.Context, req browser.ActRequest) error {
	m.acts = append(m.acts, req)
	return nil
}

func (m *mockPage) ConsoleMessages(ctx context.Context) ([]browser.ConsoleMsg, error) {
	return []browser.ConsoleMsg{
		{Type: "log", Text: "hello world", Time: time.Now()},
	}, nil
}

func TestManagerLifecycleAndTabs(t *testing.T) {
	mgr := browser.NewManager()
	if mgr.IsStarted() {
		t.Error("expected manager not to be started initially")
	}

	ctx := context.Background()

	// Register a dummy cdp engine factory for unit testing
	browser.Register("mock_test_engine", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		return &mockEngine{}, func() error { return nil }, nil
	})

	// Configure to use the mock engine
	dummyGroupCfg := &mockToolGroupCfg{
		config: `{"engine":"mock_test_engine"}`,
	}

	// OpenPage should auto-start the engine
	page, err := mgr.OpenPage(ctx, "test_scope", dummyGroupCfg, nil)
	if err != nil {
		t.Fatalf("failed to open page: %v", err)
	}
	if page == nil {
		t.Fatal("expected non-nil page")
	}

	if !mgr.IsStarted() {
		t.Error("expected manager to be started after opening page")
	}

	list, err := mgr.ListPages(ctx)
	if err != nil {
		t.Fatalf("failed to list pages: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 page in list, got %d", len(list))
	}

	activePage, err := mgr.GetActivePage()
	if err != nil {
		t.Fatalf("failed to get active page: %v", err)
	}
	if activePage == nil {
		t.Fatal("expected non-nil active page")
	}

	// Close tab
	err = mgr.ClosePage(ctx)
	if err != nil {
		t.Fatalf("failed to close page: %v", err)
	}

	list2, _ := mgr.ListPages(ctx)
	if len(list2) != 0 {
		t.Errorf("expected 0 pages after closing, got %d", len(list2))
	}

	// Stop manager
	err = mgr.Stop(ctx)
	if err != nil {
		t.Fatalf("failed to stop manager: %v", err)
	}
	if mgr.IsStarted() {
		t.Error("expected manager to be stopped")
	}
}

type mockEngine struct{}

func (m *mockEngine) Start(ctx context.Context) error { return nil }
func (m *mockEngine) Stop(ctx context.Context) error  { return nil }
func (m *mockEngine) NewContext(ctx context.Context, scope string) (browser.Context, error) {
	return &mockContext{}, nil
}

type mockContext struct{}

func (m *mockContext) Close(ctx context.Context) error { return nil }
func (m *mockContext) NewPage(ctx context.Context) (browser.Page, error) {
	return &mockPage{snapshotRes: &browser.Snapshot{AXTree: "rootWebArea", Text: "Hello"}}, nil
}
func (m *mockContext) Pages(ctx context.Context) ([]browser.Page, error) {
	return []browser.Page{&mockPage{}}, nil
}
func (m *mockContext) Cookies(ctx context.Context) ([]browser.Cookie, error) {
	return []browser.Cookie{}, nil
}
func (m *mockContext) SetCookies(ctx context.Context, cookies []browser.Cookie) error {
	return nil
}

type mockToolGroupCfg struct {
	config string
}

func (m *mockToolGroupCfg) GetConfig(ctx context.Context, category string) (string, error) {
	return m.config, nil
}
