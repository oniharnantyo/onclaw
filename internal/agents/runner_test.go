package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

func TestResolvedContextWindow(t *testing.T) {
	if got := resolvedContextWindow(nil); got != 200_000 {
		t.Fatalf("expected default context window 200000, got %d", got)
	}

	custom := 128_000
	if got := resolvedContextWindow(&custom); got != 128_000 {
		t.Fatalf("expected custom context window 128000, got %d", got)
	}
}

func TestNewRunner_DefaultsAndOptions(t *testing.T) {
	runner := NewRunner(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("dummy-key"),
		"/tmp/test-onclaw",
	)

	if runner.summarizationMargin != DefaultSummarizationMargin {
		t.Fatalf("expected default summarization margin %f, got %f", DefaultSummarizationMargin, runner.summarizationMargin)
	}
	if runner.maxIterations != 0 {
		t.Fatalf("expected default maxIterations 0, got %d", runner.maxIterations)
	}
	if runner.instructionComposer == nil {
		t.Fatal("expected default instructionComposer, got nil")
	}
	if runner.toolRegistry == nil {
		t.Fatal("expected default toolRegistry, got nil")
	}

	customRunner := NewRunner(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("dummy-key"),
		"/tmp/test-onclaw",
		WithSummarizationMargin(0.5),
		WithMaxIterations(42),
	)
	if customRunner.summarizationMargin != 0.5 {
		t.Fatalf("expected margin 0.5, got %f", customRunner.summarizationMargin)
	}
	if customRunner.maxIterations != 42 {
		t.Fatalf("expected maxIterations 42, got %d", customRunner.maxIterations)
	}
}

func TestRunner_Run_Validation(t *testing.T) {
	ctx := context.Background()
	runner := NewRunner(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil,
		[]byte("dummy-key"),
		"/tmp/test-onclaw",
	)

	tests := []struct {
		name string
		req  ExecRequest
	}{
		{name: "empty request", req: ExecRequest{}},
		{name: "missing agent_id", req: ExecRequest{WorkspaceID: "ws-1"}},
		{name: "missing session_id", req: ExecRequest{WorkspaceID: "ws-1", AgentID: "ag-1"}},
		{name: "missing user_id", req: ExecRequest{WorkspaceID: "ws-1", AgentID: "ag-1", SessionID: "sess-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runner.Run(ctx, tt.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// fakeEnabledReader is a fixed backend.EnabledSkillReader for invocation tests.
type fakeEnabledReader []string

func (f fakeEnabledReader) EnabledSkillNames(ctx context.Context, workspaceSlug string) ([]string, error) {
	return append([]string(nil), f...), nil
}

// newInvocationRunner builds a Runner with a three-tier skills tree on disk:
// system skill "sys-skill", enabled workspace skill "ws-skill", disabled
// workspace skill "ws-off", agent skill "agent-skill".
func newInvocationRunner(t *testing.T, enabled fakeEnabledReader) (*Runner, *domain.Workspace, *domain.Agent) {
	t.Helper()
	onClawDir := t.TempDir()
	writeSkill := func(dir, name string) {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatalf("mkdir skill dir: %v", err)
		}
		content := "---\nname: " + name + "\ndescription: test skill\n---\n# " + name + "\n"
		if err := os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}
	writeSkill(domain.SystemSkillsDir(onClawDir), "sys-skill")
	writeSkill(domain.WorkspaceSkillsDir(onClawDir, "acme"), "ws-skill")
	writeSkill(domain.WorkspaceSkillsDir(onClawDir, "acme"), "ws-off")
	writeSkill(domain.AgentSkillsDir(onClawDir, "acme", "ops-bot"), "agent-skill")

	r := &Runner{onClawDir: onClawDir, enabledSkillReader: enabled}
	ws := &domain.Workspace{Slug: "acme"}
	agent := &domain.Agent{Slug: "ops-bot"}
	return r, ws, agent
}

func TestRunner_InjectSkillInvocations(t *testing.T) {
	ctx := context.Background()

	t.Run("direct invocation forces skill mention", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "Summarize this report with $sys-skill please")
		if !strings.Contains(got, `invoke the skill tool with name "sys-skill"`) {
			t.Errorf("expected blocking instruction for sys-skill, got: %q", got)
		}
		if !strings.Contains(got, "Summarize this report with $sys-skill please") {
			t.Errorf("original input must be preserved after the instruction, got: %q", got)
		}
		if idxInstruction := strings.Index(got, "invoke the skill tool"); idxInstruction > strings.Index(got, "Summarize this") {
			t.Error("instruction must be injected ahead of the user message")
		}
	})

	t.Run("cron-prompt style input honors invocation", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "Morning digest: run $ws-skill over yesterday's incidents.")
		if !strings.Contains(got, `"ws-skill"`) {
			t.Errorf("expected workspace skill invocation in: %q", got)
		}
	})

	t.Run("agent-tier skill invocable", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{})

		got := r.injectSkillInvocations(ctx, ws, agent, "use $agent-skill")
		if !strings.Contains(got, `"agent-skill"`) {
			t.Errorf("expected agent skill invocation in: %q", got)
		}
	})

	t.Run("disabled workspace skill stays plain text", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "please run $ws-off now")
		if got != "please run $ws-off now" {
			t.Errorf("disabled skill mention must pass through untouched, got: %q", got)
		}
	})

	t.Run("unknown $token stays plain text", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "that costs $5 and uses $notaskill, also $HOME")
		if got != "that costs $5 and uses $notaskill, also $HOME" {
			t.Errorf("non-matching tokens must pass through untouched, got: %q", got)
		}
	})

	t.Run("multiple distinct mentions each inject once", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "$sys-skill then $sys-skill then $ws-skill")
		if c := strings.Count(got, "invoke the skill tool"); c != 2 {
			t.Errorf("expected 2 distinct invocations, got %d in: %q", c, got)
		}
	})

	t.Run("input without dollar sign untouched", func(t *testing.T) {
		r, ws, agent := newInvocationRunner(t, fakeEnabledReader{"ws-skill"})

		got := r.injectSkillInvocations(ctx, ws, agent, "just a normal message")
		if got != "just a normal message" {
			t.Errorf("plain input must be untouched, got: %q", got)
		}
	})
}
