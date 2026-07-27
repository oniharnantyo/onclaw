package cli

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/cloudwego/eino/schema"

	"github.com/oniharnantyo/onclaw/internal/config"
	"github.com/oniharnantyo/onclaw/internal/llm"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
)

func TestResolveEmbedTimeout(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty defaults to 30s", "", 30 * time.Second},
		{"invalid defaults to 30s", "not-a-duration", 30 * time.Second},
		{"zero defaults to 30s", "0s", 30 * time.Second},
		{"negative defaults to 30s", "-5s", 30 * time.Second},
		{"seconds parsed", "45s", 45 * time.Second},
		{"minutes parsed", "2m", 120 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveEmbedTimeout(tc.in); got != tc.want {
				t.Errorf("resolveEmbedTimeout(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeOllamaBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty defaults to local daemon", "", "http://localhost:11434"},
		{"bare base unchanged", "http://localhost:11434", "http://localhost:11434"},
		{"trailing slash stripped", "http://localhost:11434/", "http://localhost:11434"},
		{"openai v1 suffix stripped", "http://localhost:11434/v1", "http://localhost:11434"},
		{"v1 with trailing slash", "http://localhost:11434/v1/", "http://localhost:11434"},
		{"remote base unchanged", "https://gpu.box:11434", "https://gpu.box:11434"},
		{"remote v1 stripped", "https://gpu.box/v1", "https://gpu.box"},
		{"subpath before v1 preserved", "https://proxy.example.com/ollama/v1", "https://proxy.example.com/ollama"},
		{"only v1 collapses to default", "/v1", "http://localhost:11434"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeOllamaBaseURL(tc.in); got != tc.want {
				t.Errorf("normalizeOllamaBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveContextWindow(t *testing.T) {
	// Case 1: Agent override present
	t.Run("agent override", func(t *testing.T) {
		res := resolveContextWindow(12000, 8000, `{"context_window":4000}`)
		if res != 12000 {
			t.Errorf("expected 12000, got %d", res)
		}
	})

	// Case 2: Global config present (no agent override)
	t.Run("global config wins over model default", func(t *testing.T) {
		res := resolveContextWindow(0, 8000, `{"context_window":4000}`)
		if res != 8000 {
			t.Errorf("expected 8000, got %d", res)
		}
	})

	// Case 3: Model metadata default wins (no agent override, no global config)
	t.Run("model default wins", func(t *testing.T) {
		res := resolveContextWindow(0, 0, `{"context_window":4000}`)
		if res != 4000 {
			t.Errorf("expected 4000, got %d", res)
		}
	})

	// Case 4: Fallback to 64000
	t.Run("fallback value when none set", func(t *testing.T) {
		res := resolveContextWindow(0, 0, "")
		if res != 64000 {
			t.Errorf("expected 64000, got %d", res)
		}
	})
}

func TestResolveAndAssemble_AgentNotFound(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	cfg, _ := config.Load("")
	st := &appState{cfg: cfg}

	ctx := context.Background()
	req := agentSessionRequest{AgentName: "nonexistent"}
	convStore := sqlite.NewConversationStore(db)

	_, _, _, err = resolveAndAssemble(ctx, st, db, mgr, req, convStore, 1, nil)
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestResolveAndAssemble_ProviderDisabled(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	ctx := context.Background()
	_ = as.AddAgent(ctx, &store.Agent{
		Name:     "test-agent",
		Provider: "disabled-prov",
		Model:    "gpt-4",
	})
	_ = ps.AddProfile(ctx, &store.Profile{
		Name:    "disabled-prov",
		Enabled: 0,
	})

	cfg, _ := config.Load("")
	st := &appState{cfg: cfg}
	req := agentSessionRequest{AgentName: "test-agent"}
	convStore := sqlite.NewConversationStore(db)

	_, _, _, err = resolveAndAssemble(ctx, st, db, mgr, req, convStore, 1, nil)
	if err == nil {
		t.Error("expected error for disabled provider")
	}
}

func TestResolveAndAssemble_Success(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	ctx := context.Background()
	_ = as.AddAgent(ctx, &store.Agent{
		Name:     "test-agent",
		Provider: "mock-prov",
		Model:    "mock-model",
	})
	_ = mgr.AddProfile(ctx, &store.Profile{
		Name:         "mock-prov",
		ProviderType: "openai",
		Enabled:      1,
	})
	_ = mgr.SetSecret(ctx, "mock-prov", "sk-dummy-key")

	cfg, _ := config.Load("")
	st := &appState{cfg: cfg}
	req := agentSessionRequest{AgentName: "test-agent"}
	convStore := sqlite.NewConversationStore(db)
	convID, err := convStore.CreateConversation(ctx, "test-agent")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	ag, sessionMgr, workspacePath, err := resolveAndAssemble(ctx, st, db, mgr, req, convStore, convID, nil)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if ag == nil || sessionMgr == nil || workspacePath == "" {
		t.Errorf("unexpected nil output: ag=%v, sessionMgr=%v, workspace=%s", ag, sessionMgr, workspacePath)
	}

	// Exercise LoadHistory -> Run(messages) -> LastTurnMeta()
	history, prevID, err := sessionMgr.LoadHistory(ctx)
	if err != nil {
		t.Fatalf("load history failed: %v", err)
	}
	if prevID != "" {
		t.Errorf("expected empty prevID for new conv, got %q", prevID)
	}
	userMsg := schema.UserAgenticMessage("hello test turn")
	turnMsgs := append(history, userMsg)
	it := ag.Run(ctx, turnMsgs)
	for {
		_, ok := it.Next()
		if !ok {
			break
		}
	}
	_ = sessionMgr.LastTurnMeta()
}

func TestResolveAndAssemble_AutoSeedMasterAgent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := sqlite.Migrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	ps := sqlite.NewProfileStore(db)
	ss := sqlite.NewSecretStore(db)
	as := sqlite.NewAgentStore(db)
	km := secrets.NewKeyManager([]byte("0123456789abcdef0123456789abcdef"))
	ar := adapter.NewRegistry()
	adapter.DefaultAdapters(ar)
	mgr := llm.NewService(ps, ss, km, ar, as)

	ctx := context.Background()
	_ = mgr.AddProfile(ctx, &store.Profile{
		Name:         "mock-prov",
		ProviderType: "openai",
		Enabled:      1,
	})
	_ = mgr.SetSecret(ctx, "mock-prov", "sk-dummy-key")

	cfg, _ := config.Load("")
	st := &appState{cfg: cfg}
	req := agentSessionRequest{AgentName: "master"}
	convStore := sqlite.NewConversationStore(db)
	convID, err := convStore.CreateConversation(ctx, "master")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	ag, sessionMgr, workspacePath, err := resolveAndAssemble(ctx, st, db, mgr, req, convStore, convID, nil)
	if err != nil {
		t.Fatalf("auto-seed master agent failed: %v", err)
	}
	if ag == nil || sessionMgr == nil || workspacePath == "" {
		t.Errorf("unexpected nil output for master agent: ag=%v, sessionMgr=%v, workspace=%s", ag, sessionMgr, workspacePath)
	}
}
