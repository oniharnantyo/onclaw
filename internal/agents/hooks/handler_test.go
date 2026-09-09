package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oniharnantyo/onclaw/internal/domain"
)

// TestRegistry_Routing pins the registry's typed-error contract: unknown
// types, known-but-unregistered types, and the disabled command handler each
// get a distinguishable sentinel so the dispatcher can apply graceful-skip
// (D7) versus on_failure (D9).
func TestRegistry_Routing(t *testing.T) {
	ev := testEvent()
	hook := testHook()

	t.Run("unknown handler type", func(t *testing.T) {
		reg := NewRegistry()
		_, err := reg.Execute(context.Background(), domain.HookHandlerType("carrier-pigeon"), nil, ev, hook, time.Second)
		if !errors.Is(err, ErrUnknownHandlerType) {
			t.Errorf("err = %v, want ErrUnknownHandlerType", err)
		}
	})

	t.Run("known but unregistered types", func(t *testing.T) {
		reg := NewRegistry()
		for _, handlerType := range []domain.HookHandlerType{domain.HookHandlerMCPTool, domain.HookHandlerPrompt} {
			_, err := reg.Execute(context.Background(), handlerType, nil, ev, hook, time.Second)
			if !errors.Is(err, ErrHandlerNotRegistered) {
				t.Errorf("%s: err = %v, want ErrHandlerNotRegistered", handlerType, err)
			}
		}
	})

	t.Run("command disabled kill switch", func(t *testing.T) {
		reg := NewRegistry(WithCommandEnabled(false))
		cfg := json.RawMessage(`{"command":"true"}`)
		_, err := reg.Execute(context.Background(), domain.HookHandlerCommand, cfg, ev, hook, time.Second)
		if !errors.Is(err, ErrCommandDisabled) {
			t.Errorf("err = %v, want ErrCommandDisabled", err)
		}
	})

	t.Run("command enabled by default", func(t *testing.T) {
		reg := NewRegistry()
		cfg := json.RawMessage(`{"command":"true"}`)
		res, err := reg.Execute(context.Background(), domain.HookHandlerCommand, cfg, ev, hook, 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "allow" || res.ExitCode == nil || *res.ExitCode != 0 {
			t.Errorf("result = %+v, want allow with exit 0", res)
		}
	})
}
