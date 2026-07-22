package adapter_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/llm/adapter"
	"github.com/oniharnantyo/onclaw/internal/store"
)

func TestIsKeyless(t *testing.T) {
	if !adapter.IsKeyless("ollama") {
		t.Error("expected ollama to be keyless")
	}
	if adapter.IsKeyless("openai") {
		t.Error("expected openai not to be keyless")
	}
}

// Prompt caching defaults ON and is disabled only by an explicit
// "prompt_caching": false. A malformed Settings blob must not break the build.
func TestPromptCachingEnabled(t *testing.T) {
	cases := []struct {
		name    string
		profile *store.Profile
		want    bool
	}{
		{"empty settings defaults on", &store.Profile{Settings: ""}, true},
		{"explicit true", &store.Profile{Settings: `{"prompt_caching": true}`}, true},
		{"explicit false", &store.Profile{Settings: `{"prompt_caching": false}`}, false},
		{"non-bool ignored -> on", &store.Profile{Settings: `{"prompt_caching": "yes"}`}, true},
		{"malformed -> on", &store.Profile{Settings: `not-json`}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := adapter.PromptCachingEnabled(c.profile); got != c.want {
				t.Errorf("promptCachingEnabled = %v, want %v", got, c.want)
			}
		})
	}
}

// Disabling prompt caching must not break adapter construction.
func TestAdapterBuildWithCachingDisabled(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)
	ctx := context.Background()

	for _, provider := range []string{"anthropic", "gemini"} {
		ad, err := r.Get(provider)
		if err != nil {
			t.Fatalf("get %s: %v", provider, err)
		}
		p := &store.Profile{
			Name:         "test",
			ProviderType: provider,
			Enabled:      1,
			Settings:     `{"prompt_caching": false}`,
		}
		if _, err := ad.Build(ctx, p, "model", "test-key"); err != nil {
			t.Errorf("build %s with caching disabled: %v", provider, err)
		}
	}
}

func TestRegistryAndStub(t *testing.T) {
	r := adapter.NewRegistry()
	_, err := r.Get("stub")
	if err == nil {
		t.Error("expected Get before Register to fail")
	}

	adapter.DefaultAdapters(r)

	ad, err := r.Get("stub")
	if err != nil {
		t.Fatalf("failed to get stub: %v", err)
	}

	ctx := context.Background()
	p := &store.Profile{
		Name:         "test",
		ProviderType: "stub",
		Enabled:      1,
	}

	m, err := ad.Build(ctx, p, "model", "")
	if err != nil {
		t.Fatalf("failed to build: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil model")
	}

	resp, err := m.Generate(ctx, nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if resp == nil {
		t.Error("expected non-nil response")
	}

	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	if sr == nil {
		t.Error("expected non-nil StreamReader")
	}
	defer sr.Close()
}

func TestAdapterBuildErrors(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)

	providers := []string{"openai", "anthropic", "google", "deepseek", "qwen", "ark"}
	ctx := context.Background()

	for _, provider := range providers {
		ad, err := r.Get(provider)
		if err != nil {
			t.Fatalf("failed to get adapter %s: %v", provider, err)
		}

		// 1. Profile is disabled
		pDisabled := &store.Profile{Name: "test", ProviderType: provider, Enabled: 0}
		_, err = ad.Build(ctx, pDisabled, "model", "key")
		if err == nil {
			t.Errorf("expected error for disabled profile on provider %s", provider)
		}

		// 2. Model name is empty
		pEnabled := &store.Profile{Name: "test", ProviderType: provider, Enabled: 1}
		_, err = ad.Build(ctx, pEnabled, "", "key")
		if err == nil {
			t.Errorf("expected error for empty model name on provider %s", provider)
		}

		// 3. API key is empty
		_, err = ad.Build(ctx, pEnabled, "model", "")
		if err == nil {
			t.Errorf("expected error for empty API key on provider %s", provider)
		}

		// 4. Invalid settings JSON
		pBadSettings := &store.Profile{Name: "test", ProviderType: provider, Enabled: 1, Settings: "bad-json"}
		_, err = ad.Build(ctx, pBadSettings, "model", "key")
		if err == nil {
			t.Errorf("expected error for invalid settings JSON on provider %s", provider)
		}
	}
}

func TestAdapterBuildSuccess(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)

	ctx := context.Background()

	// 1. OpenAI
	openaiAd, _ := r.Get("openai")
	openaiProfile := &store.Profile{
		Name:         "test",
		ProviderType: "openai",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "stop": ["\n"], "reasoning_effort": "high"}`,
	}
	_, err := openaiAd.Build(ctx, openaiProfile, "gpt-4", "test-key")
	if err != nil {
		t.Errorf("failed to build openai: %v", err)
	}

	// 2. Anthropic
	claudeAd, _ := r.Get("anthropic")
	claudeProfile := &store.Profile{
		Name:         "test",
		ProviderType: "anthropic",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "reasoning_effort": "medium", "reasoning_budget_tokens": 2048}`,
	}
	_, err = claudeAd.Build(ctx, claudeProfile, "claude-3-opus", "test-key")
	if err != nil {
		t.Errorf("failed to build anthropic: %v", err)
	}

	// 3. Gemini
	geminiAd, _ := r.Get("gemini")
	geminiProfile := &store.Profile{
		Name:         "test",
		ProviderType: "gemini",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "top_p": 0.9, "reasoning_effort": "medium", "reasoning_budget_tokens": 1024}`,
	}
	_, err = geminiAd.Build(ctx, geminiProfile, "gemini-1.5-pro", "test-key")
	if err != nil {
		t.Errorf("failed to build gemini: %v", err)
	}

	// 4. DeepSeek
	dsAd, _ := r.Get("deepseek")
	dsProfile := &store.Profile{
		Name:         "test",
		ProviderType: "deepseek",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100}`,
	}
	_, err = dsAd.Build(ctx, dsProfile, "deepseek-chat", "test-key")
	if err != nil {
		t.Errorf("failed to build deepseek: %v", err)
	}

	// 5. Qwen
	qwenAd, _ := r.Get("qwen")
	qwenProfile := &store.Profile{
		Name:         "test",
		ProviderType: "qwen",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "reasoning_effort": "on"}`,
	}
	_, err = qwenAd.Build(ctx, qwenProfile, "qwen-max", "test-key")
	if err != nil {
		t.Errorf("failed to build qwen: %v", err)
	}

	// 6. Ark
	arkAd, _ := r.Get("ark")
	arkProfile := &store.Profile{
		Name:         "test",
		ProviderType: "ark",
		Enabled:      1,
		Settings:     `{"temperature": 0.7, "max_tokens": 100, "reasoning_effort": "medium"}`,
	}
	_, err = arkAd.Build(ctx, arkProfile, "ark-model", "test-key")
	if err != nil {
		t.Errorf("failed to build ark: %v", err)
	}
}

// TestStubStreamEmitsIndexedDeltas verifies the stub adapter emits at least two
// delta blocks that share a stable streaming_meta.index, so streaming + the
// client-side delta merge are exercisable without a real provider.
func TestStubStreamEmitsIndexedDeltas(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)
	ad, err := r.Get("stub")
	if err != nil {
		t.Fatalf("get stub adapter: %v", err)
	}

	ctx := context.Background()
	m, err := ad.Build(ctx, &store.Profile{Name: "test", ProviderType: "stub", Enabled: 1}, "model", "")
	if err != nil {
		t.Fatalf("build stub model: %v", err)
	}

	sr, err := m.Stream(ctx, nil)
	if err != nil {
		t.Fatalf("stub Stream: %v", err)
	}
	defer sr.Close()

	var blocks []*schema.ContentBlock
	for {
		msg, err := sr.Recv()
		if err != nil {
			break
		}
		blocks = append(blocks, msg.ContentBlocks...)
	}

	if len(blocks) < 2 {
		t.Fatalf("expected >=2 delta blocks, got %d", len(blocks))
	}

	firstIdx := blocks[0].StreamingMeta.Index
	for i, b := range blocks {
		if b.StreamingMeta == nil || b.StreamingMeta.Index != firstIdx {
			t.Errorf("block %d has unstable streaming index (want %d)", i, firstIdx)
		}
	}
}

// TestCacheablePrefixMarkedAcrossTurns asserts the provider caching decision is
// stable turn-to-turn (the adapter-output proxy for "the stable prefix is marked
// cacheable and reused across turns", Req 5 scenario). Anthropic keeps caching
// enabled on every turn; Ollama (keyless, no caching) keeps building.
func TestCacheablePrefixMarkedAcrossTurns(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)
	ctx := context.Background()

	claudeAd, err := r.Get("anthropic")
	if err != nil {
		t.Fatalf("get anthropic: %v", err)
	}
	cp := &store.Profile{Name: "t", ProviderType: "anthropic", Enabled: 1}
	for turn := 1; turn <= 3; turn++ {
		if !adapter.PromptCachingEnabled(cp) {
			t.Fatalf("turn %d: expected anthropic prefix caching enabled", turn)
		}
		if _, err := claudeAd.Build(ctx, cp, "claude-3-5-sonnet", "k"); err != nil {
			t.Fatalf("turn %d: anthropic build: %v", turn, err)
		}
	}

	ollamaAd, err := r.Get("ollama")
	if err != nil {
		t.Fatalf("get ollama: %v", err)
	}
	op := &store.Profile{Name: "t", ProviderType: "ollama", Enabled: 1}
	for turn := 1; turn <= 3; turn++ {
		m, berr := ollamaAd.Build(ctx, op, "llama3", "")
		if berr != nil {
			t.Fatalf("turn %d: ollama build: %v", turn, berr)
		}
		if m == nil {
			t.Fatalf("turn %d: expected non-nil ollama model", turn)
		}
	}
}

// TestNonCachingProviderStillFunctions asserts a provider that offers no prefix
// caching (Ollama) still builds and is usable, including when the caching flag is
// explicitly disabled — the graceful-degradation path of Req 5.
func TestNonCachingProviderStillFunctions(t *testing.T) {
	r := adapter.NewRegistry()
	adapter.DefaultAdapters(r)
	ctx := context.Background()

	ollamaAd, err := r.Get("ollama")
	if err != nil {
		t.Fatalf("get ollama: %v", err)
	}

	for _, settings := range []string{"", `{"prompt_caching": false}`} {
		p := &store.Profile{Name: "t", ProviderType: "ollama", Enabled: 1, Settings: settings}
		m, berr := ollamaAd.Build(ctx, p, "llama3", "")
		if berr != nil {
			t.Fatalf("ollama build (settings=%q): %v", settings, berr)
		}
		if m == nil {
			t.Fatalf("ollama build (settings=%q): expected non-nil model", settings)
		}
	}
}
