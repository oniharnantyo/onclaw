package agents

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type dummyModel struct{}

func (m *dummyModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant}, nil
}

func (m *dummyModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	return nil, nil
}

type dummyTool struct {
	name string
}

func (d *dummyTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: d.name,
		Desc: "dummy tool for testing",
	}, nil
}

func TestCompose_Validation(t *testing.T) {
	ctx := context.Background()

	validConfig := func() *Config {
		return &Config{
			Name:        "test-agent",
			Description: "agent for testing",
			Instruction: "You are a test agent.",
			ChatModel:   &dummyModel{},
		}
	}

	tests := []struct {
		name          string
		modify        func(*Config) *Config
		wantErrSubstr string
	}{
		{
			name: "nil config",
			modify: func(c *Config) *Config {
				return nil
			},
			wantErrSubstr: "config is required",
		},
		{
			name: "missing chat model",
			modify: func(c *Config) *Config {
				c.ChatModel = nil
				return c
			},
			wantErrSubstr: "chat model",
		},
		{
			name: "empty name",
			modify: func(c *Config) *Config {
				c.Name = ""
				return c
			},
			wantErrSubstr: "name",
		},
		{
			name: "whitespace name",
			modify: func(c *Config) *Config {
				c.Name = "   \t\n"
				return c
			},
			wantErrSubstr: "name",
		},
		{
			name: "empty instruction",
			modify: func(c *Config) *Config {
				c.Instruction = ""
				return c
			},
			wantErrSubstr: "instruction",
		},
		{
			name: "whitespace instruction",
			modify: func(c *Config) *Config {
				c.Instruction = "   \n"
				return c
			},
			wantErrSubstr: "instruction",
		},
		{
			name: "summarization without filesystem",
			modify: func(c *Config) *Config {
				c.Summarization = &SummarizationConfig{TriggerTokens: 1000}
				c.Filesystem = nil
				return c
			},
			wantErrSubstr: "filesystem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.modify(validConfig())
			agent, err := Compose(ctx, cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErrSubstr)) {
				t.Fatalf("error %q does not contain expected substring %q", err.Error(), tt.wantErrSubstr)
			}
			if agent != nil {
				t.Fatalf("expected nil agent on validation failure, got %v", agent)
			}
		})
	}
}

func TestCompose_Capabilities(t *testing.T) {
	ctx := context.Background()

	t.Run("all capabilities absent", func(t *testing.T) {
		cfg := &Config{
			Name:        "agent-no-caps",
			Description: "no capabilities",
			Instruction: "You are a simple agent.",
			ChatModel:   &dummyModel{},
		}

		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 3 {
			t.Fatalf("expected exactly 3 handlers (patchtoolcalls, attachments, tool-error-result), got %d", len(handlers))
		}
		assertHandlerType(t, handlers[0], "patchtoolcalls")
		assertHandlerType(t, handlers[1], "attachmentsplaceholdermiddleware")

		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		if agent == nil {
			t.Fatal("expected non-nil agent")
		}
	})

	t.Run("filesystem only attaches reduction and filesystem", func(t *testing.T) {
		cfg := &Config{
			Name:        "agent-fs-only",
			Description: "filesystem only",
			Instruction: "You have filesystem tools.",
			ChatModel:   &dummyModel{},
			Filesystem: &FilesystemConfig{
				AgentDir: t.TempDir(),
			},
		}

		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 5 {
			t.Fatalf("expected 5 handlers (patchtoolcalls, reduction, filesystem, attachments, tool-error-result), got %d", len(handlers))
		}
		assertHandlerType(t, handlers[0], "patchtoolcalls")
		assertHandlerType(t, handlers[1], "reduction")
		assertHandlerType(t, handlers[2], "filesystem")
		assertHandlerType(t, handlers[3], "attachmentsplaceholdermiddleware")

		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		if agent == nil {
			t.Fatal("expected non-nil agent")
		}
	})

	t.Run("skills only attaches skill middleware", func(t *testing.T) {
		cfg := &Config{
			Name:        "agent-skills-only",
			Description: "skills only",
			Instruction: "You have skills.",
			ChatModel:   &dummyModel{},
			Skills: &SkillsConfig{
				OnClawDir:  t.TempDir(),
				TenantSlug: "acme",
				AgentSlug:  "ops-bot",
			},
		}

		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 4 {
			t.Fatalf("expected 4 handlers (patchtoolcalls, skill, attachments, tool-error-result), got %d", len(handlers))
		}
		assertHandlerType(t, handlers[0], "patchtoolcalls")
		assertHandlerType(t, handlers[1], "skill")

		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		if agent == nil {
			t.Fatal("expected non-nil agent")
		}
	})

	t.Run("summarization with filesystem attaches summarization", func(t *testing.T) {
		cfg := &Config{
			Name:        "agent-summ",
			Description: "summarization test",
			Instruction: "You summarize.",
			ChatModel:   &dummyModel{},
			Filesystem: &FilesystemConfig{
				AgentDir: t.TempDir(),
			},
			Summarization: &SummarizationConfig{
				TriggerTokens: 2048,
			},
		}

		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 6 {
			t.Fatalf("expected 6 handlers (patchtoolcalls, reduction, summarization, filesystem, attachments, tool-error-result), got %d", len(handlers))
		}
		assertHandlerType(t, handlers[0], "patchtoolcalls")
		assertHandlerType(t, handlers[1], "reduction")
		assertHandlerType(t, handlers[2], "summarization")
		assertHandlerType(t, handlers[3], "filesystem")
		assertHandlerType(t, handlers[4], "attachmentsplaceholdermiddleware")

		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		if agent == nil {
			t.Fatal("expected non-nil agent")
		}
	})

	t.Run("full stack preserves ordering patchtoolcalls -> reduction -> summarization -> skill -> filesystem", func(t *testing.T) {
		agentDir := t.TempDir()
		clawDir := t.TempDir()

		cfg := &Config{
			Name:        "agent-full-stack",
			Description: "full stack capabilities",
			Instruction: "You have everything.",
			ChatModel:   &dummyModel{},
			Filesystem: &FilesystemConfig{
				AgentDir: agentDir,
			},
			Skills: &SkillsConfig{
				OnClawDir:  clawDir,
				TenantSlug: "acme",
				AgentSlug:  "full-bot",
			},
			Summarization: &SummarizationConfig{
				TriggerTokens: 4096,
			},
		}

		handlers, err := buildMiddlewares(ctx, cfg)
		if err != nil {
			t.Fatalf("buildMiddlewares failed: %v", err)
		}
		if len(handlers) != 7 {
			t.Fatalf("expected 7 handlers, got %d", len(handlers))
		}

		expectedOrder := []string{"patchtoolcalls", "reduction", "summarization", "skill", "filesystem", "attachmentsplaceholdermiddleware"}
		for i, exp := range expectedOrder {
			assertHandlerType(t, handlers[i], exp)
		}

		agent, err := Compose(ctx, cfg)
		if err != nil {
			t.Fatalf("Compose failed: %v", err)
		}
		if agent == nil {
			t.Fatal("expected non-nil agent")
		}
	})
}

func TestCompose_IterationDefault(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		maxIterations int
	}{
		{name: "zero defaults to package default 25", maxIterations: 0},
		{name: "negative defaults to package default 25", maxIterations: -10},
		{name: "explicit positive preserved", maxIterations: 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Name:          "agent-iter",
				Description:   "agent testing max iterations",
				Instruction:   "You are an iteration tester.",
				ChatModel:     &dummyModel{},
				MaxIterations: tt.maxIterations,
			}
			agent, err := Compose(ctx, cfg)
			if err != nil {
				t.Fatalf("Compose failed: %v", err)
			}
			if agent == nil {
				t.Fatal("expected non-nil agent")
			}
		})
	}
}

func TestCompose_DeterministicAndSuppliedTools(t *testing.T) {
	ctx := context.Background()

	customTools := []tool.BaseTool{
		&dummyTool{name: "custom.action"},
		&dummyTool{name: "custom.query"},
	}

	cfg := &Config{
		Name:        "agent-deterministic",
		Description: "deterministic agent description",
		Instruction: "deterministic instruction",
		ChatModel:   &dummyModel{},
		Tools:       customTools,
	}

	agent1, err := Compose(ctx, cfg)
	if err != nil {
		t.Fatalf("first Compose failed: %v", err)
	}

	agent2, err := Compose(ctx, cfg)
	if err != nil {
		t.Fatalf("second Compose failed: %v", err)
	}

	if agent1.Name(ctx) != agent2.Name(ctx) || agent1.Name(ctx) != cfg.Name {
		t.Fatalf("expected identical names %q, got %q and %q", cfg.Name, agent1.Name(ctx), agent2.Name(ctx))
	}
	if agent1.Description(ctx) != agent2.Description(ctx) || agent1.Description(ctx) != cfg.Description {
		t.Fatalf("expected identical descriptions %q, got %q and %q", cfg.Description, agent1.Description(ctx), agent2.Description(ctx))
	}
}

func assertHandlerType(t *testing.T, h adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], expectedSubstr string) {
	t.Helper()
	typeNameStr := strings.ToLower(fmt.Sprintf("%T", h))
	if !strings.Contains(typeNameStr, expectedSubstr) {
		t.Fatalf("expected middleware type containing %q, got %s", expectedSubstr, typeNameStr)
	}
}
