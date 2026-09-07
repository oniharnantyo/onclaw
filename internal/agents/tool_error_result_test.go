package agents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/agents/backend"
)

func TestToolErrorResultMiddleware_ConvertsErrorsToResults(t *testing.T) {
	mw := newToolErrorResultMiddleware()

	failing := func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
		return "", errors.New("web.fetch: target returned status 404")
	}
	endpoint, err := mw.WrapInvokableToolCall(context.Background(), failing, &adk.ToolContext{Name: "web.fetch", CallID: "call_1"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	out, err := endpoint(context.Background(), "{}")
	if err != nil {
		t.Fatalf("tool error must not propagate: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("error result must be JSON, got %q: %v", out, err)
	}
	if decoded["tool"] != "web.fetch" {
		t.Errorf("expected tool name in result, got %v", decoded)
	}
	if decoded["error"] != "web.fetch: target returned status 404" {
		t.Errorf("expected error text in result, got %v", decoded)
	}
}

func TestToolErrorResultMiddleware_SuccessAndCancellationPassthrough(t *testing.T) {
	mw := newToolErrorResultMiddleware()

	endpoint, err := mw.WrapInvokableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (string, error) { return "ok", nil },
		&adk.ToolContext{Name: "t"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	out, err := endpoint(context.Background(), "{}")
	if err != nil || out != "ok" {
		t.Fatalf("successful calls must pass through untouched, got %q, %v", out, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelledEndpoint, err := mw.WrapInvokableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (string, error) { return "", ctx.Err() },
		&adk.ToolContext{Name: "t"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if _, err := cancelledEndpoint(ctx, "{}"); err == nil {
		t.Fatal("cancellation must stay a real error, not a result")
	}
}

func TestToolErrorResultMiddleware_InterruptSignalsPassThrough(t *testing.T) {
	mw := newToolErrorResultMiddleware()

	endpoint, err := mw.WrapInvokableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
			return "", tool.Interrupt(ctx, backend.ShellApprovalInfo{Command: "rm -rf /tmp/x"})
		},
		&adk.ToolContext{Name: "execute", CallID: "call_9"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	out, err := endpoint(context.Background(), "{}")
	// The approval interrupt is control flow: it must reach the ADK as an
	// error so the run pauses for consent, not become a tool result.
	var interrupt *adk.InterruptSignal
	if !errors.As(err, &interrupt) {
		t.Fatalf("expected interrupt signal passthrough, got result %q err %v", out, err)
	}
}

// The ToolsNode executes tool calls through the STREAMABLE endpoint (the
// error text is "failed to stream tool call") — the invokable wrapper alone
// never sees the failure.
func TestToolErrorResultMiddleware_StreamableErrorsBecomeResults(t *testing.T) {
	mw := newToolErrorResultMiddleware()

	endpoint, err := mw.WrapStreamableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, errors.New("web.fetch: target returned status 404")
		},
		&adk.ToolContext{Name: "web.fetch", CallID: "call_2"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}

	sr, err := endpoint(context.Background(), "{}")
	if err != nil {
		t.Fatalf("streamable tool error must not propagate: %v", err)
	}
	var collected string
	for {
		chunk, rerr := sr.Recv()
		if rerr != nil {
			break
		}
		collected += chunk
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(collected), &decoded); err != nil {
		t.Fatalf("error result stream must be JSON, got %q: %v", collected, err)
	}
	if decoded["error"] != "web.fetch: target returned status 404" || decoded["tool"] != "web.fetch" {
		t.Errorf("unexpected error result: %v", decoded)
	}
}

func TestToolErrorResultMiddleware_StreamablePassthroughAndInterrupts(t *testing.T) {
	mw := newToolErrorResultMiddleware()

	endpoint, err := mw.WrapStreamableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
			sr, sender := schema.Pipe[string](1)
			_ = sender.Send("out", nil)
			sender.Close()
			return sr, nil
		},
		&adk.ToolContext{Name: "execute"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	sr, err := endpoint(context.Background(), "{}")
	if err != nil {
		t.Fatalf("successful stream must pass through: %v", err)
	}
	chunk, rerr := sr.Recv()
	if rerr != nil || chunk != "out" {
		t.Fatalf("expected frame passthrough, got %q, %v", chunk, rerr)
	}

	interruptEndpoint, err := mw.WrapStreamableToolCall(context.Background(),
		func(ctx context.Context, _ string, _ ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, tool.Interrupt(ctx, backend.ShellApprovalInfo{Command: "ls /tmp"})
		},
		&adk.ToolContext{Name: "execute"})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if _, err := interruptEndpoint(context.Background(), "{}"); err == nil {
		t.Fatal("interrupt from streamable endpoint must stay an error")
	}
}
