package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/providers"
	"github.com/oniharnantyo/onclaw/internal/secrets"
	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/fake"
)

type mockChatModel struct {
	generateFunc func(ctx context.Context, input []*schema.Message) (*schema.Message, error)
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, input)
	}
	return nil, errors.New("not implemented")
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream not implemented in mock")
}

// promptsToolCallMessage builds a model response carrying the prompts as a
// tool call — the shape some adapters surface structured output as.
func promptsToolCallMessage(identity, soul, bootstrap string) *schema.Message {
	args, _ := json.Marshal(GeneratedPrompts{Identity: identity, Soul: soul, Bootstrap: bootstrap})
	return &schema.Message{
		ToolCalls: []schema.ToolCall{{
			ID:       "call_prompts",
			Type:     "function",
			Function: schema.FunctionCall{Name: "submit_prompts", Arguments: string(args)},
		}},
	}
}

func TestEmbeddedBasePrompt(t *testing.T) {
	if len(BasePrompt) == 0 {
		t.Fatal("expected embedded BasePrompt to be non-empty")
	}
	if !strings.Contains(BasePrompt, "OnClaw Agent Base System Prompt (L1)") {
		t.Errorf("BasePrompt missing header: %s", BasePrompt)
	}
	if !strings.Contains(BasePrompt, "Workspace & Tenant Boundaries") {
		t.Errorf("BasePrompt missing workspace boundaries section")
	}
}

func TestBuildGenerationPrompt(t *testing.T) {
	agent := &domain.Agent{
		Name:        "Data Analyst",
		Role:        "sql-specialist",
		Description: "Analyzes tenant metrics",
		Brief:       "Focus on precision and actionable summaries.",
	}

	prompt := BuildGenerationPrompt(agent)
	if !strings.Contains(prompt, "Data Analyst") {
		t.Errorf("expected name in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "sql-specialist") {
		t.Errorf("expected role in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "Analyzes tenant metrics") {
		t.Errorf("expected description in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "Focus on precision and actionable summaries.") {
		t.Errorf("expected brief in prompt, got %s", prompt)
	}

	msgs := BuildGenerationMessages(agent, nil, "")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system, user), got %d", len(msgs))
	}
	if msgs[0].Role != schema.System || !strings.Contains(msgs[0].Content, "IDENTITY.md") {
		t.Errorf("unexpected system message: %v", msgs[0])
	}
	if msgs[1].Role != schema.User || !strings.Contains(msgs[1].Content, "Data Analyst") {
		t.Errorf("unexpected user message: %v", msgs[1])
	}
}

func TestParseGenerationOutput(t *testing.T) {
	tests := []struct {
		name          string
		resp          *schema.Message
		wantIdentity  string
		wantSoul      string
		wantBootstrap string
		wantErr       bool
	}{
		{
			name:          "forced tool call arguments",
			resp:          promptsToolCallMessage("# Identity\nCode reviewer.", "# Soul\nConcise and friendly.", "# BOOTSTRAP.md - Birth Sequence\nYou just woke up."),
			wantIdentity:  "# Identity\nCode reviewer.",
			wantSoul:      "# Soul\nConcise and friendly.",
			wantBootstrap: "# BOOTSTRAP.md - Birth Sequence\nYou just woke up.",
		},
		{
			name:          "json object in content",
			resp:          &schema.Message{Content: `{"identity":"# Identity\nExpert architect.","soul":"# Soul\nThoughtful mentor.","bootstrap":"# BOOTSTRAP.md - Birth Sequence\nIntroduce yourself."}`},
			wantIdentity:  "# Identity\nExpert architect.",
			wantSoul:      "# Soul\nThoughtful mentor.",
			wantBootstrap: "# BOOTSTRAP.md - Birth Sequence\nIntroduce yourself.",
		},
		{
			name:          "fenced json in content",
			resp:          &schema.Message{Content: "```json\n{\"identity\":\"# Identity\\nRelentless debugger.\",\"soul\":\"# Soul\\nCalm under pressure.\",\"bootstrap\":\"# BOOTSTRAP.md - Birth Sequence\\nShow your vibe.\"}\n```"},
			wantIdentity:  "# Identity\nRelentless debugger.",
			wantSoul:      "# Soul\nCalm under pressure.",
			wantBootstrap: "# BOOTSTRAP.md - Birth Sequence\nShow your vibe.",
		},
		{
			name:          "prose around json in content",
			resp:          &schema.Message{Content: "Here is the result:\n{\"identity\":\"I analyze incidents.\",\"soul\":\"I answer with calm.\",\"bootstrap\":\"I say hello and ask for the first task.\"}\nHope that helps."},
			wantIdentity:  "I analyze incidents.",
			wantSoul:      "I answer with calm.",
			wantBootstrap: "I say hello and ask for the first task.",
		},
		{
			name:          "bare markdown documents with prose",
			resp:          &schema.Message{Content: "Sure, here are the documents:\n\n# IDENTITY.md - Who Am I?\n**Name:** Radar\n\n## Competencies\nIncident analysis.\n\n# SOUL.md\nShort beats long.\n\n# BOOTSTRAP.md - Birth Sequence\n_You just woke up._\n"},
			wantIdentity:  "# IDENTITY.md - Who Am I?\n**Name:** Radar\n\n## Competencies\nIncident analysis.",
			wantSoul:      "# SOUL.md\nShort beats long.",
			wantBootstrap: "# BOOTSTRAP.md - Birth Sequence\n_You just woke up._",
		},
		{
			name:          "h2 markdown headings",
			resp:          &schema.Message{Content: "## IDENTITY.md\nWho am I?\n\n## SOUL.md\nVoice.\n\n## BOOTSTRAP.md\nWake up."},
			wantIdentity:  "## IDENTITY.md\nWho am I?",
			wantSoul:      "## SOUL.md\nVoice.",
			wantBootstrap: "## BOOTSTRAP.md\nWake up.",
		},
		{
			name:    "markdown headings out of order",
			resp:    &schema.Message{Content: "# SOUL.md\nVoice.\n\n# IDENTITY.md\nWho.\n\n# BOOTSTRAP.md\nWake."},
			wantErr: true,
		},
		{
			name:    "markdown missing bootstrap heading",
			resp:    &schema.Message{Content: "# IDENTITY.md\nWho.\n\n# SOUL.md\nVoice."},
			wantErr: true,
		},
		{
			name:    "empty identity rejected",
			resp:    promptsToolCallMessage("", "# Soul\nSomeone.", "# BOOTSTRAP.md\nSomeone."),
			wantErr: true,
		},
		{
			name:    "empty soul rejected",
			resp:    promptsToolCallMessage("# Identity\nSomeone.", "   ", "# BOOTSTRAP.md\nSomeone."),
			wantErr: true,
		},
		{
			name:    "empty bootstrap rejected",
			resp:    promptsToolCallMessage("# Identity\nSomeone.", "# Soul\nSomeone.", "  "),
			wantErr: true,
		},
		{
			name:    "invalid json arguments",
			resp:    &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "submit_prompts", Arguments: "not json"}}}},
			wantErr: true,
		},
		{
			name:    "missing soul key",
			resp:    &schema.Message{Content: `{"identity":"only identity"}`},
			wantErr: true,
		},
		{
			name:    "missing bootstrap key",
			resp:    &schema.Message{Content: `{"identity":"I analyze.","soul":"I answer."}`},
			wantErr: true,
		},
		{
			name:    "nil response",
			resp:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIdentity, gotSoul, gotBootstrap, err := ParseGenerationOutput(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGenerationOutput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if gotIdentity != tt.wantIdentity {
					t.Errorf("gotIdentity = %q, want %q", gotIdentity, tt.wantIdentity)
				}
				if gotSoul != tt.wantSoul {
					t.Errorf("gotSoul = %q, want %q", gotSoul, tt.wantSoul)
				}
				if gotBootstrap != tt.wantBootstrap {
					t.Errorf("gotBootstrap = %q, want %q", gotBootstrap, tt.wantBootstrap)
				}
			}
		})
	}
}

// TestService_Generate_TimesOut proves the service timeout is enforced: a model
// call that hangs past the configured budget fails the generation with the
// timeout reason instead of hanging the request indefinitely.
func TestService_Generate_TimesOut(t *testing.T) {
	ctx := context.Background()
	st := fake.New()
	key := []byte("01234567890123456789012345678901") // 32 bytes AES key

	hangingModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return promptsToolCallMessage("# Identity\\nSomeone.", "# Soul\\nSomeone.", "# BOOTSTRAP.md\\nSomeone."), nil
			}
		},
	}
	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		return hangingModel, nil
	}
	svc := NewService(st, key, WithAgentPromptGeneratorModelFactory(factory), WithTimeout(50*time.Millisecond))

	ws := &domain.Workspace{ID: "ws-timeout", Slug: "ws-timeout", Name: "WS Timeout"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-secret-provider-key"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	p := &domain.ProviderConfig{
		ID:            "prov-timeout",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "hang")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-timeout",
		WorkspaceID:   ws.ID,
		Slug:          "hang",
		Name:          "Hanging Agent",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	err = svc.Generate(ctx, wsDir, ws.ID, agent.ID, "")
	if err == nil || !strings.Contains(err.Error(), "prompt generation timed out — retry") {
		t.Fatalf("expected timeout failure, got %v", err)
	}

	stored, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if stored.PromptsStatus != domain.PromptsStatusFailed {
		t.Errorf("status = %q, want failed", stored.PromptsStatus)
	}
	if stored.PromptsError == nil || *stored.PromptsError != "prompt generation timed out — retry" {
		t.Errorf("prompts_error = %v, want the timeout reason", stored.PromptsError)
	}

	// No documents may have been written by the aborted generation.
	if _, err := os.ReadFile(filepath.Join(wsDir, "IDENTITY.md")); !os.IsNotExist(err) {
		t.Errorf("IDENTITY.md should not exist after a timed-out generation, got err %v", err)
	}
}

// deadlineAwareAgents fails prompt-state writes made with an expired context,
// the way a real pgx-backed store does; all other calls delegate.
type deadlineAwareAgents struct {
	store.AgentStore
}

func (a *deadlineAwareAgents) SetPromptState(ctx context.Context, workspaceID, id string, status domain.PromptsStatus, promptsErr *string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.AgentStore.SetPromptState(ctx, workspaceID, id, status, promptsErr)
}

// deadlineAwareStore wraps the fake store so context-deadline violations on
// prompt-state writes surface like they would against PostgreSQL.
type deadlineAwareStore struct {
	store.Store
}

func (s *deadlineAwareStore) Agents() store.AgentStore {
	return &deadlineAwareAgents{s.Store.Agents()}
}

// TestService_Generate_RecordsFailureAfterContextDeath pins the timeout failure
// mode the web app observed: when the generation context dies (request
// deadline, client disconnect), the failed state must still be recorded — the
// terminal write uses a detached context instead of the dead one.
func TestService_Generate_RecordsFailureAfterContextDeath(t *testing.T) {
	ctx := context.Background()
	st := &deadlineAwareStore{Store: fake.New()}
	key := []byte("01234567890123456789012345678901") // 32 bytes AES key

	dyingModel := &mockChatModel{
		generateFunc: func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return promptsToolCallMessage("# Identity\\nSomeone.", "# Soul\\nSomeone.", "# BOOTSTRAP.md\\nSomeone."), nil
		},
	}
	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		return dyingModel, nil
	}
	// 30ms budget: the caller context below outlives the generation budget by
	// dying first, mirroring a handler deadline that expires mid-generation.
	svc := NewService(st, key, WithAgentPromptGeneratorModelFactory(factory), WithTimeout(30*time.Millisecond))

	ws := &domain.Workspace{ID: "ws-deadctx", Slug: "ws-deadctx", Name: "WS DeadCtx"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-secret-provider-key"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	p := &domain.ProviderConfig{
		ID:            "prov-deadctx",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "deadctx")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-deadctx",
		WorkspaceID:   ws.ID,
		Slug:          "deadctx",
		Name:          "DeadCtx Agent",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// The caller's context dies before generation starts — a request deadline
	// that expired mid-generation. Without a detached terminal write, the
	// failure cannot be recorded and the row stays 'generating' forever.
	callerCtx, cancelCaller := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancelCaller()
	time.Sleep(20 * time.Millisecond)

	err = svc.Generate(callerCtx, wsDir, ws.ID, agent.ID, "")
	if err == nil || !strings.Contains(err.Error(), "prompt generation timed out — retry") {
		t.Fatalf("expected timeout failure, got %v", err)
	}

	stored, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if stored.PromptsStatus != domain.PromptsStatusFailed {
		t.Errorf("status = %q, want failed", stored.PromptsStatus)
	}
	if stored.PromptsError == nil || *stored.PromptsError != "prompt generation timed out — retry" {
		t.Errorf("prompts_error = %v, want the timeout reason", stored.PromptsError)
	}
}

func TestAgentPromptGeneratorModelFactory(t *testing.T) {
	ctx := context.Background()

	validTypes := []struct {
		pType string
		cred  providers.Credential
		model string
	}{
		{
			pType: providers.TypeOpenAI,
			cred:  providers.Credential{APIKey: "sk-test-openai"},
			model: "gpt-4o",
		},
		{
			pType: providers.TypeAnthropic,
			cred:  providers.Credential{APIKey: "sk-ant-test"},
			model: "claude-3-5-sonnet-20241022",
		},
		{
			pType: providers.TypeGemini,
			cred:  providers.Credential{APIKey: "gemini-key"},
			model: "gemini-1.5-pro",
		},
		{
			pType: providers.TypeOpenRouter,
			cred:  providers.Credential{APIKey: "sk-or-test"},
			model: "anthropic/claude-3.5-sonnet",
		},
		{
			pType: providers.TypeOpenAICompatible,
			cred:  providers.Credential{APIKey: "sk-test", BaseURL: "https://custom-ai.com/v1"},
			model: "local-model",
		},
		{
			pType: providers.TypeAnthropicCompatible,
			cred:  providers.Credential{APIKey: "sk-test", BaseURL: "https://custom-claude.com/v1"},
			model: "claude-custom",
		},
	}

	for _, tt := range validTypes {
		t.Run(tt.pType, func(t *testing.T) {
			m, err := AgentPromptGeneratorModelFactory(ctx, tt.pType, tt.cred, tt.model)
			if err != nil {
				t.Fatalf("AgentPromptGeneratorModelFactory(%s) returned unexpected error: %v", tt.pType, err)
			}
			if m == nil {
				t.Fatalf("AgentPromptGeneratorModelFactory(%s) returned nil model", tt.pType)
			}
		})
	}

	t.Run("unsupported type", func(t *testing.T) {
		_, err := AgentPromptGeneratorModelFactory(ctx, "unknown-provider", providers.Credential{}, "model")
		if err == nil || !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected ErrInvalid for unknown provider, got %v", err)
		}
	})
}

func setupTestService(t *testing.T, mockGen func(ctx context.Context, input []*schema.Message) (*schema.Message, error)) (*Service, store.Store, []byte) {
	t.Helper()
	st := fake.New()
	key := []byte("01234567890123456789012345678901") // 32 bytes AES key

	mockModel := &mockChatModel{
		generateFunc: mockGen,
	}

	factory := func(ctx context.Context, providerType string, cred providers.Credential, modelName string) (model.BaseChatModel, error) {
		return mockModel, nil
	}

	svc := NewService(st, key, WithAgentPromptGeneratorModelFactory(factory), WithTimeout(5*time.Second))
	return svc, st, key
}

func TestService_Generate_Success(t *testing.T) {
	ctx := context.Background()
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		return promptsToolCallMessage("# Identity\nYou are a DevOps engineer.", "# Soul\nYou are calm under pressure.", "# BOOTSTRAP.md - Birth Sequence\nSay hi, then ask for the first task."), nil
	})

	ws := &domain.Workspace{ID: "ws-1", Slug: "ws-1", Name: "WS 1"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-secret-provider-key"))
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}

	p := &domain.ProviderConfig{
		ID:            "prov-1",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "devops")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-1",
		WorkspaceID:   ws.ID,
		Slug:          "devops",
		Name:          "DevOps Agent",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		Brief:         "Automate cloud deployments",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Generate synchronously
	if err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, ""); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify agent updated in store
	updated, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch updated agent: %v", err)
	}

	if updated.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("expected PromptsStatus = %s, got %s", domain.PromptsStatusReady, updated.PromptsStatus)
	}
	if updated.PromptsError != nil {
		t.Errorf("expected PromptsError = nil, got %v", *updated.PromptsError)
	}

	// The generated documents live in the workspace dir, not the store.
	for name, want := range map[string]string{
		"IDENTITY.md":  "DevOps engineer",
		"SOUL.md":      "calm under pressure",
		"BOOTSTRAP.md": "Say hi, then ask for the first task.",
	} {
		got, err := os.ReadFile(filepath.Join(wsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(string(got), want) {
			t.Errorf("%s = %q, want it to contain %q", name, string(got), want)
		}
	}
}

func TestService_Generate_Failures(t *testing.T) {
	ctx := context.Background()

	t.Run("provider not found", func(t *testing.T) {
		svc, st, key := setupTestService(t, nil)
		ws := &domain.Workspace{ID: "ws-err1", Slug: "ws-err1", Name: "WS Err 1"}
		_ = st.Workspaces().Create(ctx, ws)

		encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
		p := &domain.ProviderConfig{
			ID:            "prov-err1",
			WorkspaceID:   ws.ID,
			Type:          providers.TypeOpenAI,
			Name:          "OpenAI",
			KeyCiphertext: encKey,
			Enabled:       true,
		}
		_ = st.Providers().Create(ctx, p)

		agent := &domain.Agent{
			ID:            "agent-err1",
			WorkspaceID:   ws.ID,
			Slug:          "agent-err1",
			Name:          "Agent",
			ProviderID:    p.ID,
			Model:         "gpt-4o",
			PromptsStatus: domain.PromptsStatusGenerating,
		}
		_ = st.Agents().Create(ctx, agent)

		// Delete provider so it's missing
		_ = st.Providers().Delete(ctx, ws.ID, p.ID)

		err := svc.Generate(ctx, t.TempDir(), ws.ID, agent.ID, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		updated, _ := st.Agents().ByID(ctx, ws.ID, agent.ID)
		if updated.PromptsStatus != domain.PromptsStatusFailed {
			t.Errorf("expected failed status, got %s", updated.PromptsStatus)
		}
		if updated.PromptsError == nil || !strings.Contains(*updated.PromptsError, "provider configuration not found") {
			t.Errorf("expected provider not found message, got %v", updated.PromptsError)
		}
	})

	t.Run("auth failure from provider", func(t *testing.T) {
		svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return nil, errors.New("401 Unauthorized: Invalid API key provided")
		})

		ws := &domain.Workspace{ID: "ws-auth", Slug: "ws-auth", Name: "WS Auth"}
		_ = st.Workspaces().Create(ctx, ws)

		encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-invalid"))
		p := &domain.ProviderConfig{
			ID:            "prov-auth",
			WorkspaceID:   ws.ID,
			Type:          providers.TypeAnthropic,
			Name:          "Anthropic",
			KeyCiphertext: encKey,
			Enabled:       true,
		}
		_ = st.Providers().Create(ctx, p)

		agent := &domain.Agent{
			ID:            "agent-auth",
			WorkspaceID:   ws.ID,
			Slug:          "agent-auth",
			Name:          "Agent Auth",
			ProviderID:    p.ID,
			Model:         "claude-3-5-sonnet-20241022",
			PromptsStatus: domain.PromptsStatusGenerating,
		}
		_ = st.Agents().Create(ctx, agent)

		err := svc.Generate(ctx, t.TempDir(), ws.ID, agent.ID, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		updated, _ := st.Agents().ByID(ctx, ws.ID, agent.ID)
		if updated.PromptsStatus != domain.PromptsStatusFailed {
			t.Errorf("expected failed status, got %s", updated.PromptsStatus)
		}
		if updated.PromptsError == nil || !strings.Contains(*updated.PromptsError, "authentication failed") {
			t.Errorf("expected authentication error message, got %v", updated.PromptsError)
		}
	})

	t.Run("decryption failure", func(t *testing.T) {
		svc, st, _ := setupTestService(t, nil)
		ws := &domain.Workspace{ID: "ws-dec", Slug: "ws-dec", Name: "WS Dec"}
		_ = st.Workspaces().Create(ctx, ws)

		p := &domain.ProviderConfig{
			ID:            "prov-dec",
			WorkspaceID:   ws.ID,
			Type:          providers.TypeOpenAI,
			Name:          "OpenAI",
			KeyCiphertext: "v1:bad-nonce:bad-ciphertext",
			Enabled:       true,
		}
		_ = st.Providers().Create(ctx, p)

		agent := &domain.Agent{
			ID:            "agent-dec",
			WorkspaceID:   ws.ID,
			Slug:          "agent-dec",
			Name:          "Agent Dec",
			ProviderID:    p.ID,
			Model:         "gpt-4o",
			PromptsStatus: domain.PromptsStatusGenerating,
		}
		_ = st.Agents().Create(ctx, agent)

		err := svc.Generate(ctx, t.TempDir(), ws.ID, agent.ID, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		updated, _ := st.Agents().ByID(ctx, ws.ID, agent.ID)
		if updated.PromptsStatus != domain.PromptsStatusFailed {
			t.Errorf("expected failed status, got %s", updated.PromptsStatus)
		}
		if updated.PromptsError == nil || !strings.Contains(*updated.PromptsError, "failed to decrypt") {
			t.Errorf("expected decryption error message, got %v", updated.PromptsError)
		}
	})

	t.Run("parse failure", func(t *testing.T) {
		svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
			return &schema.Message{Content: "Just plain text with no JSON object"}, nil
		})

		ws := &domain.Workspace{ID: "ws-parse", Slug: "ws-parse", Name: "WS Parse"}
		_ = st.Workspaces().Create(ctx, ws)

		encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
		p := &domain.ProviderConfig{
			ID:            "prov-parse",
			WorkspaceID:   ws.ID,
			Type:          providers.TypeOpenAI,
			Name:          "OpenAI",
			KeyCiphertext: encKey,
			Enabled:       true,
		}
		_ = st.Providers().Create(ctx, p)

		wsDir := filepath.Join(t.TempDir(), "agents", "agent-parse")
		if err := SeedWorkspace(wsDir); err != nil {
			t.Fatalf("seed workspace dir: %v", err)
		}

		agent := &domain.Agent{
			ID:            "agent-parse",
			WorkspaceID:   ws.ID,
			Slug:          "agent-parse",
			Name:          "Agent Parse",
			ProviderID:    p.ID,
			Model:         "gpt-4o",
			PromptsStatus: domain.PromptsStatusGenerating,
			}
		_ = st.Agents().Create(ctx, agent)

		err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		updated, _ := st.Agents().ByID(ctx, ws.ID, agent.ID)
		if updated.PromptsStatus != domain.PromptsStatusFailed {
			t.Errorf("expected failed status, got %s", updated.PromptsStatus)
		}
		if updated.PromptsError == nil || !strings.Contains(*updated.PromptsError, "failed to parse") {
			t.Errorf("expected parse error message, got %v", updated.PromptsError)
		}

		// No prompt documents may land on disk when parsing fails.
		for _, name := range []string{"IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
			if _, err := os.Stat(filepath.Join(wsDir, name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s must not exist after parse failure, stat err = %v", name, err)
			}
		}
	})

	t.Run("agent deleted before generation", func(t *testing.T) {
		svc, _, _ := setupTestService(t, nil)
		err := svc.Generate(ctx, t.TempDir(), "ws-none", "agent-none", "")
		if err != nil {
			t.Fatalf("expected nil when agent not found, got %v", err)
		}
	})
}

func TestService_DeleteDuringFlight(t *testing.T) {
	ctx := context.Background()
	var st store.Store

	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		// Delete agent from store mid-generation
		_ = st.Agents().Delete(ctx, "ws-flight", "agent-flight")
		return promptsToolCallMessage("# Identity", "# Soul", "# BOOTSTRAP"), nil
	})

	ws := &domain.Workspace{ID: "ws-flight", Slug: "ws-flight", Name: "WS Flight"}
	_ = st.Workspaces().Create(ctx, ws)

	encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	p := &domain.ProviderConfig{
		ID:            "prov-flight",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	_ = st.Providers().Create(ctx, p)

	wsDir := filepath.Join(t.TempDir(), "agents", "agent-flight")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-flight",
		WorkspaceID:   ws.ID,
		Slug:          "agent-flight",
		Name:          "Agent Flight",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	_ = st.Agents().Create(ctx, agent)

	err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, "")
	if err != nil {
		t.Fatalf("expected graceful no-op on delete-during-flight, got err: %v", err)
	}
}

func TestService_Sweep(t *testing.T) {
	ctx := context.Background()
	svc, st, key := setupTestService(t, nil)

	ws := &domain.Workspace{ID: "ws-sweep", Slug: "ws-sweep", Name: "WS Sweep"}
	_ = st.Workspaces().Create(ctx, ws)

	encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	p := &domain.ProviderConfig{
		ID:            "prov-sweep",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	_ = st.Providers().Create(ctx, p)

	// Agent 1: generating
	a1 := &domain.Agent{
		ID:            "a1",
		WorkspaceID:   ws.ID,
		Slug:          "a1",
		Name:          "A1",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	_ = st.Agents().Create(ctx, a1)

	// Agent 2: generating
	a2 := &domain.Agent{
		ID:            "a2",
		WorkspaceID:   ws.ID,
		Slug:          "a2",
		Name:          "A2",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	_ = st.Agents().Create(ctx, a2)

	// Agent 3: ready
	a3 := &domain.Agent{
		ID:            "a3",
		WorkspaceID:   ws.ID,
		Slug:          "a3",
		Name:          "A3",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusReady,
		Identity:      "existing",
		Soul:          "existing",
	}
	_ = st.Agents().Create(ctx, a3)

	count, err := svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 agents swept, got %d", count)
	}

	r1, _ := st.Agents().ByID(ctx, ws.ID, "a1")
	if r1.PromptsStatus != domain.PromptsStatusFailed || *r1.PromptsError != InterruptedErrorMessage {
		t.Errorf("a1 not properly swept: status=%s, err=%v", r1.PromptsStatus, r1.PromptsError)
	}

	r2, _ := st.Agents().ByID(ctx, ws.ID, "a2")
	if r2.PromptsStatus != domain.PromptsStatusFailed || *r2.PromptsError != InterruptedErrorMessage {
		t.Errorf("a2 not properly swept: status=%s, err=%v", r2.PromptsStatus, r2.PromptsError)
	}

	r3, _ := st.Agents().ByID(ctx, ws.ID, "a3")
	if r3.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("a3 status should remain ready, got %s", r3.PromptsStatus)
	}
}

func TestService_GeneratePersistsReadyState(t *testing.T) {
	ctx := context.Background()

	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		return promptsToolCallMessage("Generated identity", "Generated soul", "Generated bootstrap"), nil
	})

	ws := &domain.Workspace{ID: "ws-kick", Slug: "ws-kick", Name: "WS Kick"}
	_ = st.Workspaces().Create(ctx, ws)

	encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	p := &domain.ProviderConfig{
		ID:            "prov-kick",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	_ = st.Providers().Create(ctx, p)

	wsDir := filepath.Join(t.TempDir(), "agents", "agent-kick")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-kick",
		WorkspaceID:   ws.ID,
		Slug:          "agent-kick",
		Name:          "Agent Kick",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusGenerating,
	}
	_ = st.Agents().Create(ctx, agent)

	if err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	updated, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch updated agent: %v", err)
	}
	if updated.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("expected status ready, got %s", updated.PromptsStatus)
	}

	// Ready implies all three documents on disk.
	for _, name := range []string{"IDENTITY.md", "SOUL.md", "BOOTSTRAP.md"} {
		if _, err := os.Stat(filepath.Join(wsDir, name)); err != nil {
			t.Errorf("expected %s in workspace dir after ready transition: %v", name, err)
		}
	}
}

func TestSanitizeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "401 unauthorized",
			err:  errors.New("HTTP 401 Unauthorized: Invalid API key"),
			want: "provider authentication failed — check provider API key",
		},
		{
			name: "403 forbidden",
			err:  errors.New("403 Forbidden: permission_denied"),
			want: "provider permission denied — check API key permissions",
		},
		{
			name: "404 not found",
			err:  errors.New("Error 404: model_not_found: The model does not exist"),
			want: "configured model not found or inaccessible with provider credentials",
		},
		{
			name: "429 rate limit",
			err:  errors.New("429 Too Many Requests: Rate limit reached"),
			want: "provider rate limit or quota exceeded — retry later",
		},
		{
			name: "timeout",
			err:  context.DeadlineExceeded,
			want: "prompt generation timed out — retry",
		},
		{
			name: "decrypt error",
			err:  errors.New("failed to decrypt ciphertext"),
			want: "failed to decrypt provider credentials",
		},
		{
			name: "parse error",
			err:  errors.New("unable to parse output sections"),
			want: "failed to parse generated identity and soul prompts from model output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeError(tt.err)
			if got != tt.want {
				t.Errorf("SanitizeError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestService_Generate_FailedRegenerationPreservesFiles(t *testing.T) {
	ctx := context.Background()

	// The model call fails on every attempt (rate limit).
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("429 Too Many Requests: Rate limit reached")
	})

	ws := &domain.Workspace{ID: "ws-preserve", Slug: "ws-preserve", Name: "WS Preserve"}
	_ = st.Workspaces().Create(ctx, ws)

	encKey, _ := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	p := &domain.ProviderConfig{
		ID:            "prov-preserve",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	_ = st.Providers().Create(ctx, p)

	// A previously ready agent with documents already on disk.
	wsDir := filepath.Join(t.TempDir(), "agents", "agent-preserve")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}
	if err := WritePromptDocuments(wsDir, "old identity", "old soul", "old bootstrap"); err != nil {
		t.Fatalf("write existing documents: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-preserve",
		WorkspaceID:   ws.ID,
		Slug:          "agent-preserve",
		Name:          "Agent Preserve",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusReady,
	}
	_ = st.Agents().Create(ctx, agent)

	if err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, ""); err == nil {
		t.Fatal("expected generation to fail")
	}

	updated, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if updated.PromptsStatus != domain.PromptsStatusFailed {
		t.Errorf("expected failed status, got %s", updated.PromptsStatus)
	}

	identity, soul, bootstrap, err := ReadPromptDocuments(wsDir)
	if err != nil {
		t.Fatalf("read back documents: %v", err)
	}
	if identity != "old identity" || soul != "old soul" || bootstrap != "old bootstrap" {
		t.Errorf("failed regeneration must preserve old documents, got identity=%q soul=%q bootstrap=%q", identity, soul, bootstrap)
	}
}

func TestBuildGenerationMessages_EnhanceMode(t *testing.T) {
	agent := &domain.Agent{
		Name:  "Radar",
		Role:  "pricing-monitor",
		Brief: "Watches competitor pricing pages.",
	}
	current := &GeneratedPrompts{
		Identity:  "# old identity body",
		Soul:      "# old soul body",
		Bootstrap: "# old bootstrap body",
	}

	msgs := BuildGenerationMessages(agent, current, "")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system, user), got %d", len(msgs))
	}
	if msgs[0].Role != schema.System || !strings.Contains(msgs[0].Content, "ENHANCE") {
		t.Errorf("enhance mode must use the enhance system framing, got %q", msgs[0].Content)
	}

	user := msgs[1].Content
	for _, want := range []string{
		"Current IDENTITY.md",
		"# old identity body",
		"Current SOUL.md",
		"# old soul body",
		"Current BOOTSTRAP.md",
		"# old bootstrap body",
		"Radar",
		"Watches competitor pricing pages.",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("enhance user prompt missing %q", want)
		}
	}
}

func TestReadCurrentPrompts(t *testing.T) {
	t.Run("nil when the workspace has no documents", func(t *testing.T) {
		if got := readCurrentPrompts(t.TempDir()); got != nil {
			t.Errorf("readCurrentPrompts() = %v, want nil for an empty workspace", got)
		}
	})

	t.Run("nil when the directory does not exist", func(t *testing.T) {
		if got := readCurrentPrompts(filepath.Join(t.TempDir(), "missing")); got != nil {
			t.Errorf("readCurrentPrompts() = %v, want nil for a missing directory", got)
		}
	})

	t.Run("documents present", func(t *testing.T) {
		dir := t.TempDir()
		if err := WritePromptDocuments(dir, "id body", "soul body", "boot body"); err != nil {
			t.Fatalf("write documents: %v", err)
		}
		got := readCurrentPrompts(dir)
		if got == nil {
			t.Fatal("readCurrentPrompts() = nil, want the documents")
		}
		if got.Identity != "id body" || got.Soul != "soul body" || got.Bootstrap != "boot body" {
			t.Errorf("readCurrentPrompts() = %+v, want the document contents", got)
		}
	})
}

func TestService_Generate_EnhancesExistingDocuments(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		captured = input
		return promptsToolCallMessage("enhanced identity", "enhanced soul", "enhanced bootstrap"), nil
	})

	ws := &domain.Workspace{ID: "ws-enhance", Slug: "ws-enhance", Name: "WS Enhance"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	p := &domain.ProviderConfig{
		ID:            "prov-enhance",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "agent-enhance")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}
	if err := WritePromptDocuments(wsDir, "old identity", "old soul", "old bootstrap"); err != nil {
		t.Fatalf("write existing documents: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-enhance",
		WorkspaceID:   ws.ID,
		Slug:          "agent-enhance",
		Name:          "Agent Enhance",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusReady,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, ""); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// The model call carried the current documents in enhance mode.
	if len(captured) != 2 {
		t.Fatalf("expected the model call to be captured, got %d messages", len(captured))
	}
	if !strings.Contains(captured[0].Content, "ENHANCE") {
		t.Errorf("expected the enhance system framing, got %q", captured[0].Content)
	}
	for _, want := range []string{"Current IDENTITY.md", "old identity", "Current SOUL.md", "old soul", "Current BOOTSTRAP.md", "old bootstrap"} {
		if !strings.Contains(captured[1].Content, want) {
			t.Errorf("enhance user prompt missing %q", want)
		}
	}

	// The enhanced documents are committed and the previous generation is
	// preserved as .bak beside them.
	identity, soul, bootstrap, err := ReadPromptDocuments(wsDir)
	if err != nil {
		t.Fatalf("read documents: %v", err)
	}
	if identity != "enhanced identity" || soul != "enhanced soul" || bootstrap != "enhanced bootstrap" {
		t.Errorf("expected enhanced documents on disk, got (%q, %q, %q)", identity, soul, bootstrap)
	}
	backup, err := os.ReadFile(filepath.Join(wsDir, "IDENTITY.md.bak"))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != "old identity" {
		t.Errorf("IDENTITY.md.bak = %q, want the previous generation", string(backup))
	}

	updated, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if updated.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("expected ready status, got %s", updated.PromptsStatus)
	}
}

func TestBuildGenerationMessages_InstructionInFreshMode(t *testing.T) {
	agent := &domain.Agent{Name: "Atlas"}
	msgs := BuildGenerationMessages(agent, nil, "keep it terse")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[1].Content, "Requested Changes") || !strings.Contains(msgs[1].Content, "keep it terse") {
		t.Errorf("fresh prompt must carry the instruction, got %q", msgs[1].Content)
	}
}

func TestService_Generate_InstructionSteersEnhancement(t *testing.T) {
	ctx := context.Background()

	var captured []*schema.Message
	svc, st, key := setupTestService(t, func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		captured = input
		return promptsToolCallMessage("enhanced identity", "enhanced soul", "enhanced bootstrap"), nil
	})

	ws := &domain.Workspace{ID: "ws-instr", Slug: "ws-instr", Name: "WS Instr"}
	if err := st.Workspaces().Create(ctx, ws); err != nil {
		t.Fatalf("create ws: %v", err)
	}

	encKey, err := secrets.Encrypt(key, []byte(ws.ID), []byte("sk-valid"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	p := &domain.ProviderConfig{
		ID:            "prov-instr",
		WorkspaceID:   ws.ID,
		Type:          providers.TypeOpenAI,
		Name:          "OpenAI",
		KeyCiphertext: encKey,
		Enabled:       true,
	}
	if err := st.Providers().Create(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	wsDir := filepath.Join(t.TempDir(), "agents", "agent-instr")
	if err := SeedWorkspace(wsDir); err != nil {
		t.Fatalf("seed workspace dir: %v", err)
	}
	if err := WritePromptDocuments(wsDir, "old identity", "old soul", "old bootstrap"); err != nil {
		t.Fatalf("write existing documents: %v", err)
	}

	agent := &domain.Agent{
		ID:            "agent-instr",
		WorkspaceID:   ws.ID,
		Slug:          "agent-instr",
		Name:          "Agent Instr",
		ProviderID:    p.ID,
		Model:         "gpt-4o",
		PromptsStatus: domain.PromptsStatusReady,
	}
	if err := st.Agents().Create(ctx, agent); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if err := svc.Generate(ctx, wsDir, ws.ID, agent.ID, "focus more on incident triage"); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for _, want := range []string{"Requested Changes", "focus more on incident triage"} {
		if !strings.Contains(captured[1].Content, want) {
			t.Errorf("enhance prompt missing %q", want)
		}
	}

	updated, err := st.Agents().ByID(ctx, ws.ID, agent.ID)
	if err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	if updated.PromptsStatus != domain.PromptsStatusReady {
		t.Errorf("expected ready status, got %s", updated.PromptsStatus)
	}
}
