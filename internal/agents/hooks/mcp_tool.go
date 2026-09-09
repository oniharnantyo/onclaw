package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// MCPInvoker is the mcp_tool handler's connectivity seam (D11): invoke one
// tool on a WORKSPACE-level MCP server. Implementations MUST resolve
// serverID against the workspace server registry only — an agent-private
// server id is unreachable by contract, indistinguishable from an unknown id.
// The concrete adapter lives in internal/agents/mcp (which must not import
// this package); tests inject fakes.
type MCPInvoker interface {
	InvokeWorkspaceTool(ctx context.Context, workspaceID, serverID, toolName string, argsJSON []byte) (resultText string, err error)
}

// mcpToolHookConfig is the decrypted mcp_tool handler config shape (see the
// pinned shapes in secrets.go): a workspace server id, one of its tools, and
// the structured input whose STRING values may carry the closed placeholder
// set below. Non-string input values pass through verbatim. Config keys
// follow Claude Code vocabulary (D21): "server" and "tool".
//
//	mcp_tool: {"server":"…","tool":"…","input":{"key":"value with ${placeholders}"}}
type mcpToolHookConfig struct {
	Server string                     `json:"server"`
	Tool   string                     `json:"tool"`
	Input  map[string]json.RawMessage `json:"input,omitempty"`
}

// hookPlaceholderPattern matches one ${...} occurrence. The inner expression
// is matched against the closed set; anything else stays literal (D11:
// string substitution, not a template engine — unknown forms must survive so
// typos are visible in the delivered payload instead of vanishing).
var hookPlaceholderPattern = regexp.MustCompile(`\$\{([^}]*)\}`)

// executeMCPTool invokes the configured workspace MCP tool per D11: the
// closed placeholder set is substituted into the input's string values, the
// input is delivered as the tool's JSON arguments, and the tool's TEXT result
// is parsed through the shared strict decision rule. A successful call
// without a decision object means allow; an MCP or invoke error is a failure
// for the hook's on_failure policy. Cold first connections can consume much
// of the budget (the MCPManager caches the connection thereafter).
func (r *Registry) executeMCPTool(ctx context.Context, cfg json.RawMessage, ev Event, _ HookRef, _ time.Duration) (Result, error) {
	var conf mcpToolHookConfig
	if err := json.Unmarshal(cfg, &conf); err != nil {
		return Result{}, fmt.Errorf("hook mcp_tool: decode config: %w", err)
	}
	if strings.TrimSpace(conf.Server) == "" {
		return Result{}, fmt.Errorf("hook mcp_tool: config.server is required")
	}
	if strings.TrimSpace(conf.Tool) == "" {
		return Result{}, fmt.Errorf("hook mcp_tool: config.tool is required")
	}

	input, err := substituteMCPInput(conf.Input, ev)
	if err != nil {
		return Result{}, fmt.Errorf("hook mcp_tool: %w", err)
	}
	argsJSON, err := json.Marshal(input)
	if err != nil {
		return Result{}, fmt.Errorf("hook mcp_tool: encode input: %w", err)
	}

	resultText, err := r.mcpInvoker.InvokeWorkspaceTool(ctx, ev.Workspace.ID, conf.Server, conf.Tool, argsJSON)
	if err != nil {
		return Result{}, fmt.Errorf("hook mcp_tool: invoke %q on server %q: %w", conf.Tool, conf.Server, err)
	}
	if decision, ok := ParseDecisionJSON([]byte(resultText)); ok {
		return Result{Decision: decision.Decision, Reason: decision.Reason}, nil
	}
	// A successful tool call whose text result is not a decision object
	// (notifications, prose, log output) means allow.
	return Result{Decision: "allow"}, nil
}

// substituteMCPInput applies the closed placeholder set to every string value
// of the input map, leaving keys, structure, and non-string values verbatim.
func substituteMCPInput(input map[string]json.RawMessage, ev Event) (map[string]json.RawMessage, error) {
	if len(input) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	out := make(map[string]json.RawMessage, len(input))
	for key, raw := range input {
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			substituted := substituteHookPlaceholders(value, ev)
			encoded, err := json.Marshal(substituted)
			if err != nil {
				return nil, fmt.Errorf("encode input %q: %w", key, err)
			}
			out[key] = encoded
			continue
		}
		out[key] = raw // non-string value: verbatim
	}
	return out, nil
}

// substituteHookPlaceholders replaces every occurrence of the closed
// placeholder set with plain string substitution. Unknown ${...} forms stay
// literal.
func substituteHookPlaceholders(value string, ev Event) string {
	return hookPlaceholderPattern.ReplaceAllStringFunc(value, func(match string) string {
		inner := match[2 : len(match)-1] // strip "${" and "}"
		switch {
		case inner == "event.tool.name":
			if ev.Tool == nil {
				return ""
			}
			return ev.Tool.Name
		case strings.HasPrefix(inner, "event.tool.args."):
			return toolArgFieldValue(ev, strings.TrimPrefix(inner, "event.tool.args."))
		case inner == "event.agent.name":
			return ev.Agent.Name
		case inner == "event.origin":
			return ev.Origin
		default:
			return match
		}
	})
}

// toolArgFieldValue reads one field from the event's RAW tool-arguments JSON.
// A missing field, absent tool, or non-object args yield the empty string
// (D11); string fields are delivered as-is, other JSON values as their JSON
// text.
func toolArgFieldValue(ev Event, field string) string {
	if ev.Tool == nil || ev.Tool.Args == "" {
		return ""
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ev.Tool.Args), &args); err != nil {
		return ""
	}
	raw, ok := args[field]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}
