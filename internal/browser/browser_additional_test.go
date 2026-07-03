package browser_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/browser"
)

type mockKVStore struct {
	data map[string]string
}

func (m *mockKVStore) Get(ctx context.Context, key string) (string, error) {
	return m.data[key], nil
}

func (m *mockKVStore) Set(ctx context.Context, key string, val string) error {
	m.data[key] = val
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

type errEngine struct {
	startErr      error
	stopErr       error
	newContextErr error
}

func (e *errEngine) Start(ctx context.Context) error { return e.startErr }
func (e *errEngine) Stop(ctx context.Context) error  { return e.stopErr }
func (e *errEngine) NewContext(ctx context.Context, scope string) (browser.Context, error) {
	if e.newContextErr != nil {
		return nil, e.newContextErr
	}
	return &mockContext{}, nil
}

func TestBrowserManager_EdgeCases(t *testing.T) {
	ctx := context.Background()

	// Register engines with failures
	browser.Register("mock_err_launch", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		return nil, nil, errors.New("launch error")
	})

	browser.Register("mock_err_start", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		return &errEngine{startErr: errors.New("start error")}, func() error { return nil }, nil
	})

	browser.Register("mock_err_context", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
		return &errEngine{newContextErr: errors.New("context error")}, func() error { return nil }, nil
	})

	t.Run("invalid config JSON", func(t *testing.T) {
		mgr := browser.NewManager()
		badGroupCfg := &mockToolGroupCfg{config: `{invalid-json`}
		err := mgr.Start(ctx, "scope", badGroupCfg, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to parse browser config") {
			t.Errorf("expected parse error, got %v", err)
		}
	})

	t.Run("unsupported engine", func(t *testing.T) {
		mgr := browser.NewManager()
		badGroupCfg := &mockToolGroupCfg{config: `{"engine":"unknown_engine"}`}
		err := mgr.Start(ctx, "scope", badGroupCfg, nil)
		if err == nil || !strings.Contains(err.Error(), "unsupported browser engine") {
			t.Errorf("expected unsupported engine error, got %v", err)
		}
	})

	t.Run("factory launch error", func(t *testing.T) {
		mgr := browser.NewManager()
		groupCfg := &mockToolGroupCfg{config: `{"engine":"mock_err_launch"}`}
		err := mgr.Start(ctx, "scope", groupCfg, nil)
		if err == nil || !strings.Contains(err.Error(), "engine unavailable") {
			t.Errorf("expected engine unavailable, got %v", err)
		}
	})

	t.Run("engine start error", func(t *testing.T) {
		mgr := browser.NewManager()
		groupCfg := &mockToolGroupCfg{config: `{"engine":"mock_err_start"}`}
		err := mgr.Start(ctx, "scope", groupCfg, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to connect engine") {
			t.Errorf("expected connect error, got %v", err)
		}
	})

	t.Run("engine newContext error", func(t *testing.T) {
		mgr := browser.NewManager()
		groupCfg := &mockToolGroupCfg{config: `{"engine":"mock_err_context"}`}
		err := mgr.Start(ctx, "scope", groupCfg, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to create context") {
			t.Errorf("expected create context error, got %v", err)
		}
	})

	t.Run("SetActivePage and GetActivePage validation", func(t *testing.T) {
		mgr := browser.NewManager()

		// 1. GetActivePage when not started
		_, err := mgr.GetActivePage()
		if err == nil || !strings.Contains(err.Error(), "browser engine is not started") {
			t.Errorf("expected not started error, got %v", err)
		}

		// 2. SetActivePage when not started
		err = mgr.SetActivePage(0)
		if err == nil || !strings.Contains(err.Error(), "browser engine is not started") {
			t.Errorf("expected not started error, got %v", err)
		}

		// Register a working engine for this test
		browser.Register("mock_active_test", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
			return &mockEngine{}, func() error { return nil }, nil
		})
		groupCfg := &mockToolGroupCfg{config: `{"engine":"mock_active_test"}`}

		err = mgr.Start(ctx, "scope", groupCfg, nil)
		if err != nil {
			t.Fatalf("unexpected start err: %v", err)
		}

		// 3. GetActivePage when started but no pages
		_, err = mgr.GetActivePage()
		if err == nil || !strings.Contains(err.Error(), "no active pages open") {
			t.Errorf("expected no active pages error, got %v", err)
		}

		// 4. SetActivePage out of bounds (no pages)
		err = mgr.SetActivePage(0)
		if err == nil || !strings.Contains(err.Error(), "invalid page index") {
			t.Errorf("expected invalid page index error, got %v", err)
		}

		// Open two pages
		_, err = mgr.OpenPage(ctx, "scope", groupCfg, nil)
		if err != nil {
			t.Fatalf("failed to open page 1: %v", err)
		}
		_, err = mgr.OpenPage(ctx, "scope", groupCfg, nil)
		if err != nil {
			t.Fatalf("failed to open page 2: %v", err)
		}

		// 5. SetActivePage out of bounds (negative index)
		err = mgr.SetActivePage(-1)
		if err == nil || !strings.Contains(err.Error(), "invalid page index") {
			t.Errorf("expected invalid page index error, got %v", err)
		}

		// 6. SetActivePage out of bounds (index 2)
		err = mgr.SetActivePage(2)
		if err == nil || !strings.Contains(err.Error(), "invalid page index") {
			t.Errorf("expected invalid page index error, got %v", err)
		}

		// 7. SetActivePage success
		err = mgr.SetActivePage(0)
		if err != nil {
			t.Errorf("unexpected set active page err: %v", err)
		}

		// 8. Close active page at 0, readjust activeIdx
		err = mgr.ClosePage(ctx)
		if err != nil {
			t.Errorf("unexpected close page err: %v", err)
		}
	})

	t.Run("Stop error handling and KVStore cookie save", func(t *testing.T) {
		kv := &mockKVStore{data: make(map[string]string)}

		// Populate cookies
		cookieData := `[{"name":"foo","value":"bar"}]`
		kv.data["browser.session.scope-test"] = cookieData

		mgr := browser.NewManager()

		// Custom factory returning engine that fails to Stop
		errStop := errors.New("stop error")
		browser.Register("mock_err_stop_engine", func(ctx context.Context, cfg browser.Config) (browser.Engine, func() error, error) {
			return &errEngine{stopErr: errStop}, func() error { return errors.New("launcher stop error") }, nil
		})

		groupCfg := &mockToolGroupCfg{config: `{"engine":"mock_err_stop_engine"}`}
		err := mgr.Start(ctx, "scope-test", groupCfg, kv)
		if err != nil {
			t.Fatalf("unexpected start error: %v", err)
		}

		// Stop should return first error (the stopErr)
		err = mgr.Stop(ctx)
		if err == nil || !errors.Is(err, errStop) {
			t.Errorf("expected stop error, got %v", err)
		}
	})
}
