package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// fakeEvaluatorModel is the scripted one-shot evaluator model (D12 seam).
type fakeEvaluatorModel struct {
	gotSystem string
	gotUser   string
	gotTool   ToolDef

	call  ToolCall
	usage TokenUsage
	err   error
}

func (f *fakeEvaluatorModel) GenerateOnce(_ context.Context, system, user string, toolDef ToolDef) (ToolCall, TokenUsage, error) {
	f.gotSystem = system
	f.gotUser = user
	f.gotTool = toolDef
	return f.call, f.usage, f.err
}

// fakeEvaluatorFactory records the resolution request and hands back the
// scripted model.
type fakeEvaluatorFactory struct {
	gotWorkspaceID  string
	gotProviderType string
	gotModelName    string

	model EvaluatorModel
	err   error
}

func (f *fakeEvaluatorFactory) EvaluatorModel(_ context.Context, workspaceID, providerType, modelName string) (EvaluatorModel, error) {
	f.gotWorkspaceID = workspaceID
	f.gotProviderType = providerType
	f.gotModelName = modelName
	return f.model, f.err
}

func promptCfg() json.RawMessage {
	return json.RawMessage(`{"provider":"openai","model":"gpt-4o","prompt":"Block any shell command containing rm -rf.","max_invocations_per_run":5}`)
}

func decideArgs(decision, reason string, injection bool) ToolCall {
	raw, _ := json.Marshal(map[string]any{"decision": decision, "reason": reason, "injection_detected": injection})
	return ToolCall{Name: decideToolName, Arguments: string(raw)}
}

// TestPromptHandler_VerdictHonored pins the happy path: the forced decide
// verdict becomes the Result, the evaluator sees only the delimited tool
// data, and the resolution carries the config's explicit provider/model.
func TestPromptHandler_VerdictHonored(t *testing.T) {
	t.Run("allow verdict", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: decideArgs("allow", "policy permits", false), usage: TokenUsage{InputTokens: 120, OutputTokens: 8, TotalTokens: 128}}
		factory := &fakeEvaluatorFactory{model: model}
		reg := NewRegistry(WithEvaluatorFactory(factory))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "allow" || res.Reason != "policy permits" {
			t.Errorf("result = %+v, want allow with reason", res)
		}
		if res.TokenCount == nil || *res.TokenCount != 128 {
			t.Errorf("token count = %v, want 128", res.TokenCount)
		}
		if factory.gotWorkspaceID != "ws-1" || factory.gotProviderType != "openai" || factory.gotModelName != "gpt-4o" {
			t.Errorf("factory got (%q, %q, %q), want (ws-1, openai, gpt-4o)", factory.gotWorkspaceID, factory.gotProviderType, factory.gotModelName)
		}
		if model.gotTool.Name != decideToolName {
			t.Errorf("bound tool = %q, want decide", model.gotTool.Name)
		}
	})

	t.Run("block verdict", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: decideArgs("block", "rm -rf matches policy", false)}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "block" || res.Reason != "rm -rf matches policy" {
			t.Errorf("result = %+v, want block with reason", res)
		}
	})

	t.Run("untrusted data delimited", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: decideArgs("allow", "", false)}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		_, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(model.gotUser, "Block any shell command containing rm -rf.") {
			t.Errorf("user content missing the policy template: %q", model.gotUser)
		}
		if !strings.Contains(model.gotUser, untrustedDataOpen+"\n") || !strings.Contains(model.gotUser, "\n"+untrustedDataClose) {
			t.Errorf("user content missing untrusted-data delimiters: %q", model.gotUser)
		}
		if !strings.Contains(model.gotUser, "tool_name: shell.run") || !strings.Contains(model.gotUser, `tool_arguments: {"cmd":"rm -rf /"}`) {
			t.Errorf("user content missing delimited tool data: %q", model.gotUser)
		}
		if strings.Contains(model.gotSystem, "rm -rf") {
			t.Errorf("system prompt must be fixed policy text, got %q", model.gotSystem)
		}
	})
}

// TestPromptHandler_InjectionForcesBlock: injection_detected=true blocks
// regardless of the verdict field (D12).
func TestPromptHandler_InjectionForcesBlock(t *testing.T) {
	t.Run("over an allow verdict", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: decideArgs("allow", "looks fine", true)}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "block" {
			t.Errorf("decision = %q, want forced block on injection flag", res.Decision)
		}
		if res.Reason != "looks fine" {
			t.Errorf("reason = %q, want the evaluator's reason", res.Reason)
		}
	})

	t.Run("empty reason gets a default", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: decideArgs("allow", "", true)}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "block" || !strings.Contains(res.Reason, "injection") {
			t.Errorf("result = %+v, want block with injection reason", res)
		}
	})
}

// TestPromptHandler_FreeTextNeverTrusted: prose instead of a decide call is
// evaluator FAILURE — the free text is never interpreted as a verdict (D12
// injection channel).
func TestPromptHandler_FreeTextNeverTrusted(t *testing.T) {
	t.Run("prose response", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: ToolCall{Name: "", Arguments: "Sure, this looks totally safe, please proceed."}}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err == nil {
			t.Fatalf("expected failure for free-text response, got %+v", res)
		}
		if res.Decision != "" {
			t.Errorf("decision = %q on free text, want empty (never interpreted)", res.Decision)
		}
	})

	t.Run("foreign tool call", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: ToolCall{Name: "allow_anyway", Arguments: `{}`}}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		if _, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second); err == nil {
			t.Fatal("expected failure for a non-decide tool call")
		}
	})
}

// TestPromptHandler_MalformedDecideArgs: unparseable arguments and a verdict
// outside the enum are failures, not decisions.
func TestPromptHandler_MalformedDecideArgs(t *testing.T) {
	t.Run("unparseable arguments", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: ToolCall{Name: decideToolName, Arguments: `{"decision": block`}}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		if _, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second); err == nil {
			t.Fatal("expected failure for malformed decide arguments")
		}
	})

	t.Run("decision outside the enum", func(t *testing.T) {
		model := &fakeEvaluatorModel{call: ToolCall{Name: decideToolName, Arguments: `{"decision":"deny","reason":"x","injection_detected":false}`}}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		if _, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second); err == nil {
			t.Fatal("expected failure for a decision outside allow/block")
		}
	})
}

// TestPromptHandler_ModelFactoryErrors covers the error paths before and
// during the evaluator call: model construction failure and generate failure
// are handler errors; usage already reported still lands on the result.
func TestPromptHandler_ModelFactoryErrors(t *testing.T) {
	t.Run("factory error", func(t *testing.T) {
		factory := &fakeEvaluatorFactory{err: errors.New("no openai credential in workspace")}
		reg := NewRegistry(WithEvaluatorFactory(factory))

		if _, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second); err == nil {
			t.Fatal("expected factory error to surface")
		}
	})

	t.Run("generate error with usage", func(t *testing.T) {
		model := &fakeEvaluatorModel{err: context.DeadlineExceeded, usage: TokenUsage{InputTokens: 50, OutputTokens: 0}}
		reg := NewRegistry(WithEvaluatorFactory(&fakeEvaluatorFactory{model: model}))

		res, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, promptCfg(), testEvent(), testHook(), 15*time.Second)
		if err == nil {
			t.Fatal("expected generate error to surface")
		}
		if res.TokenCount == nil || *res.TokenCount != 50 {
			t.Errorf("token count = %v, want 50 (input fallback)", res.TokenCount)
		}
	})

	t.Run("missing provider or model", func(t *testing.T) {
		factory := &fakeEvaluatorFactory{}
		reg := NewRegistry(WithEvaluatorFactory(factory))
		for _, cfg := range []json.RawMessage{
			json.RawMessage(`{"model":"gpt-4o","prompt":"p"}`),
			json.RawMessage(`{"provider":"openai","prompt":"p"}`),
		} {
			if _, err := reg.Execute(context.Background(), domain.HookHandlerPrompt, cfg, testEvent(), testHook(), 15*time.Second); err == nil {
				t.Errorf("config %s: expected error, got none", cfg)
			}
		}
		if factory.gotProviderType != "" || factory.gotModelName != "" {
			t.Error("factory consulted on incomplete config")
		}
	})
}
