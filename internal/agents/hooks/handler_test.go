package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const testBudget = 10 * time.Second

// testEvent is the payload pushed to stdin; tests mutate it to probe
// truncation and fidelity.
func testEvent() Event {
	return Event{
		Event:      "user_prompt_submit",
		DeliveryID: "cmd-delivery-7",
		Origin:     "scheduler",
		Agent:      EventRef{ID: "ag-1", Name: "Atlas"},
		SessionID:  "sess-9",
	}
}

// testHook is the hook ref the handler tests execute under.
func testHook() HookRef {
	return HookRef{Name: "guard", Tier: TierGlobal}
}

// runCommandShell executes a command hook whose config is the legacy shell
// form {"command": script} — no args, so the handler runs the line via
// "sh -c" (today's row shape keeps working without a data migration).
func runCommandShell(t *testing.T, script string, budget time.Duration, opts ...RegistryOption) (Result, error) {
	t.Helper()
	raw, err := json.Marshal(commandHookConfig{Command: script})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return NewRegistry(opts...).Execute(context.Background(), HandlerCommand, raw, testEvent(), testHook(), budget)
}

// runCommandExec executes a command hook in exec form: {"command":"sh",
// "args":["-c", script]} — the form for hooks that must not pass through a
// shell line.
func runCommandExec(t *testing.T, script string, budget time.Duration, opts ...RegistryOption) (Result, error) {
	t.Helper()
	raw, err := json.Marshal(commandHookConfig{Command: "sh", Args: []string{"-c", script}})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return NewRegistry(opts...).Execute(context.Background(), HandlerCommand, raw, testEvent(), testHook(), budget)
}

func mustJSONString(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func intPtr(i int) *int { return &i }

// TestRegistry_Routing pins the registry's typed-error contract: unknown
// types and the kill-switched handlers each get a distinguishable sentinel so
// the dispatcher can apply graceful-skip versus on_failure.
func TestRegistry_Routing(t *testing.T) {
	ev := testEvent()
	hook := testHook()

	t.Run("unknown handler type", func(t *testing.T) {
		_, err := NewRegistry().Execute(context.Background(), "carrier-pigeon", nil, ev, hook, time.Second)
		if !errors.Is(err, ErrUnknownHandlerType) {
			t.Errorf("err = %v, want ErrUnknownHandlerType", err)
		}
	})

	t.Run("command disabled kill switch", func(t *testing.T) {
		reg := NewRegistry(WithCommandEnabled(false))
		cfg := json.RawMessage(`{"command":"true"}`)
		_, err := reg.Execute(context.Background(), HandlerCommand, cfg, ev, hook, time.Second)
		if !errors.Is(err, ErrCommandDisabled) {
			t.Errorf("err = %v, want ErrCommandDisabled", err)
		}
	})

	t.Run("command enabled by default", func(t *testing.T) {
		cfg := json.RawMessage(`{"command":"true"}`)
		res, err := NewRegistry().Execute(context.Background(), HandlerCommand, cfg, ev, hook, 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != DecisionAllow || res.ExitCode == nil || *res.ExitCode != 0 {
			t.Errorf("result = %+v, want allow with exit 0", res)
		}
	})
}
