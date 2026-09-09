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

// fakeMCPInvoker captures the invocation and returns a canned result — the
// mcp_tool handler's connectivity seam (D11).
type fakeMCPInvoker struct {
	gotWorkspaceID string
	gotServerID    string
	gotToolName    string
	gotArgs        map[string]any
	invocations    int

	resultText string
	err        error
}

func (f *fakeMCPInvoker) InvokeWorkspaceTool(_ context.Context, workspaceID, serverID, toolName string, argsJSON []byte) (string, error) {
	f.invocations++
	f.gotWorkspaceID = workspaceID
	f.gotServerID = serverID
	f.gotToolName = toolName
	if len(argsJSON) > 0 {
		_ = json.Unmarshal(argsJSON, &f.gotArgs)
	}
	return f.resultText, f.err
}

func mcpToolCfg(input string) json.RawMessage {
	return json.RawMessage(`{"server":"srv-1","tool":"send_message","input":` + input + `}`)
}

// TestMCPToolHandler_DecisionFromTextResult pins the decision contract: a
// strict decision JSON in the tool's text result is honored, any other
// successful result means allow.
func TestMCPToolHandler_DecisionFromTextResult(t *testing.T) {
	t.Run("block decision honored", func(t *testing.T) {
		inv := &fakeMCPInvoker{resultText: `{"decision":"block","reason":"escalation paged"}`}
		reg := NewRegistry(WithMCPInvoker(inv))

		res, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(`{"channel":"#ops"}`), testEvent(), testHook(), 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "block" || res.Reason != "escalation paged" {
			t.Errorf("result = %+v, want block with reason", res)
		}
	})

	t.Run("allow decision honored", func(t *testing.T) {
		inv := &fakeMCPInvoker{resultText: `{"decision":"allow"}`}
		reg := NewRegistry(WithMCPInvoker(inv))

		res, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(`{}`), testEvent(), testHook(), 5*time.Second)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Decision != "allow" {
			t.Errorf("decision = %q, want allow", res.Decision)
		}
	})

	t.Run("non-decision text means allow", func(t *testing.T) {
		for _, text := range []string{"", "Message posted to #ops", `{"ok":true}`, "log line\nanother line"} {
			inv := &fakeMCPInvoker{resultText: text}
			reg := NewRegistry(WithMCPInvoker(inv))
			res, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(`{}`), testEvent(), testHook(), 5*time.Second)
			if err != nil {
				t.Fatalf("Execute(%q): %v", text, err)
			}
			if res.Decision != "allow" {
				t.Errorf("text %q: decision = %q, want allow (success without decision)", text, res.Decision)
			}
		}
	})
}

// TestMCPToolHandler_ColdConnectError covers the failure path: an invoker
// error (cold MCP connect, tool missing, invoke failure) is a handler error
// for the hook's on_failure policy — never an allow.
func TestMCPToolHandler_ColdConnectError(t *testing.T) {
	inv := &fakeMCPInvoker{err: errors.New("dial tcp 10.0.0.1:3333: connect refused")}
	reg := NewRegistry(WithMCPInvoker(inv))

	res, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(`{}`), testEvent(), testHook(), 5*time.Second)
	if err == nil {
		t.Fatalf("expected error from cold-connect failure, got result %+v", res)
	}
	if !strings.Contains(err.Error(), "connect refused") {
		t.Errorf("error = %v, want the invoker's cause", err)
	}
	if res.Decision != "" {
		t.Errorf("decision = %q on failure, want empty (caller applies on_failure)", res.Decision)
	}
}

// TestMCPToolHandler_PlaceholderSubstitution covers the closed placeholder
// set (D11) against what the invoker actually received: every form resolves,
// a missing args field is empty, an unknown ${...} stays literal, and
// non-string input values pass through verbatim.
func TestMCPToolHandler_PlaceholderSubstitution(t *testing.T) {
	inv := &fakeMCPInvoker{resultText: ""}
	reg := NewRegistry(WithMCPInvoker(inv))

	input := `{
		"text": "agent ${event.agent.name} ran ${event.tool.name} from origin ${event.origin} with cmd ${event.tool.args.cmd} and missing ${event.tool.args.nope}",
		"unknown_stays": "${event.definitely.not.a.placeholder} and $HOME",
		"count": 7,
		"flag": true
	}`
	_, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(input), testEvent(), testHook(), 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inv.invocations != 1 {
		t.Fatalf("invocations = %d, want 1", inv.invocations)
	}

	if inv.gotWorkspaceID != "ws-1" || inv.gotServerID != "srv-1" || inv.gotToolName != "send_message" {
		t.Errorf("routing = (%q, %q, %q), want (ws-1, srv-1, send_message)", inv.gotWorkspaceID, inv.gotServerID, inv.gotToolName)
	}

	text, _ := inv.gotArgs["text"].(string)
	wantText := "agent Atlas ran shell.run from origin user with cmd rm -rf / and missing "
	if text != wantText {
		t.Errorf("text = %q, want %q", text, wantText)
	}

	unknown, _ := inv.gotArgs["unknown_stays"].(string)
	if unknown != "${event.definitely.not.a.placeholder} and $HOME" {
		t.Errorf("unknown = %q, want the literal preserved", unknown)
	}

	// Non-string values ride along untouched.
	if count, ok := inv.gotArgs["count"].(float64); !ok || count != 7 {
		t.Errorf("count = %v, want 7 verbatim", inv.gotArgs["count"])
	}
	if flag, ok := inv.gotArgs["flag"].(bool); !ok || !flag {
		t.Errorf("flag = %v, want true verbatim", inv.gotArgs["flag"])
	}
}

// TestMCPToolHandler_PlaceholdersWithoutTool pins the no-tool event shape:
// tool placeholders resolve to the empty string instead of failing.
func TestMCPToolHandler_PlaceholdersWithoutTool(t *testing.T) {
	inv := &fakeMCPInvoker{resultText: ""}
	reg := NewRegistry(WithMCPInvoker(inv))

	ev := testEvent()
	ev.Tool = nil
	input := `{"note":"tool=${event.tool.name} args=${event.tool.args.cmd} agent=${event.agent.name}"}`
	if _, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, mcpToolCfg(input), ev, testHook(), 5*time.Second); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	note, _ := inv.gotArgs["note"].(string)
	if note != "tool= args= agent=Atlas" {
		t.Errorf("note = %q, want empty tool placeholders", note)
	}
}

// TestMCPToolHandler_RequiredConfigFields pins the config shape errors.
func TestMCPToolHandler_RequiredConfigFields(t *testing.T) {
	inv := &fakeMCPInvoker{}
	reg := NewRegistry(WithMCPInvoker(inv))

	for _, cfg := range []json.RawMessage{
		json.RawMessage(`{"tool":"x"}`),
		json.RawMessage(`{"server":"srv-1"}`),
		json.RawMessage(`{`),
	} {
		if _, err := reg.Execute(context.Background(), domain.HookHandlerMCPTool, cfg, testEvent(), testHook(), 5*time.Second); err == nil {
			t.Errorf("config %s: expected error, got none", cfg)
		}
	}
	if inv.invocations != 0 {
		t.Errorf("invoker reached %d times on malformed configs, want 0", inv.invocations)
	}
}
