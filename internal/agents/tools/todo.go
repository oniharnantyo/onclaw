package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// Todo tool names (adopt-assistant-ui-elements tasks 5.3/5.4): dotted
// registry names so pre_tool_use hooks target them by exact name in matchers.
const (
	NameTodoWrite = "todo_write"
	NameTodoRead  = "todo_read"
)

// todoWriteResult is the echoed stored state both todo tools return: the
// session's current items (key order) and its revision counter.
type todoWriteResult struct {
	Items    []todoResultItem `json:"items"`
	Revision int64            `json:"revision"`
}

// todoResultItem is one item's model-facing projection.
type todoResultItem struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// todoWriteTool replaces the calling session's todo list (adopt-assistant-
// ui-elements D5). Scoping is structural: the tool is constructed with the
// executing run's workspace, agent, and session identity, so no argument can
// address another session's plan. It performs no filesystem, network, or
// shell effects.
type todoWriteTool struct {
	todos       store.TodoStore
	workspaceID string
	agentID     string
	sessionID   string
}

// NewTodoWrite constructs the write tool bound to the executing run's
// identity. The store is composition-root wiring (never nil — the tool is
// registered only when wired).
func NewTodoWrite(todos store.TodoStore, workspaceID, agentID, sessionID string) (tool.BaseTool, error) {
	return &todoWriteTool{
		todos:       todos,
		workspaceID: workspaceID,
		agentID:     agentID,
		sessionID:   sessionID,
	}, nil
}

// Info returns the tool schema surfaced to agentic models. The description
// carries the rewrite contract: the call's item list IS the whole list.
func (t *todoWriteTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameTodoWrite,
		Desc: "Replace your todo list for this session with the full item list passed in — the call is the whole list, not a delta. " +
			"Each item carries a stable key (reuse a key to update that item in place; a key absent from the call deletes that item), " +
			"a short display text, a status of exactly \"pending\", \"active\", \"done\", or \"failed\", " +
			"and an optional reason (the failure explanation, for failed items). " +
			"Every call increments the list revision; an empty item list clears the plan. " +
			"Invalid lists are rejected with an error and nothing changes — fix the named item and call again. " +
			"Keep the list current: mark items active as you start them and done or failed as you finish them.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"items": {
				Type:     schema.Array,
				Desc:     `The full item list, each item an object {"key": string (required), "text": string (required), "status": "pending"|"active"|"done"|"failed" (required), "reason": string (optional)}.`,
				Required: true,
			},
		}),
	}, nil
}

// todoWriteArgs is the deserialized tool-call argument shape.
type todoWriteArgs struct {
	Items []todoArgsItem `json:"items"`
}

// todoArgsItem is one item as the model sends it. Reason arrives optional;
// an absent or empty reason stores as none.
type todoArgsItem struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues. Validation runs BEFORE the store write,
// so a rejected list never mutates stored state.
func (t *todoWriteTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args todoWriteArgs
	if err := strictDecodeArgs(NameTodoWrite, argumentsInJSON, &args); err != nil {
		return "", err
	}

	items := make([]store.TodoItem, 0, len(args.Items))
	for i, item := range args.Items {
		if strings.TrimSpace(item.Key) == "" {
			return "", fmt.Errorf("%s: item %d: key is required", NameTodoWrite, i+1)
		}
		if strings.TrimSpace(item.Text) == "" {
			return "", fmt.Errorf("%s: item %q: text is required", NameTodoWrite, item.Key)
		}
		status := store.TodoStatus(item.Status)
		if !store.ValidTodoStatus(status) {
			return "", fmt.Errorf(`%s: item %q: status must be one of "pending", "active", "done", "failed", got %q`, NameTodoWrite, item.Key, item.Status)
		}
		items = append(items, store.TodoItem{
			ItemKey:  item.Key,
			ItemText: item.Text,
			Status:   status,
			Reason:   item.Reason,
		})
	}
	// Duplicate keys cannot satisfy the store's per-item uniqueness — name
	// the first one here so the model can correct the list directly.
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, dup := seen[item.ItemKey]; dup {
			return "", fmt.Errorf("%s: duplicate item key %q", NameTodoWrite, item.ItemKey)
		}
		seen[item.ItemKey] = struct{}{}
	}

	stored, err := t.todos.Replace(ctx, &store.TodoList{
		WorkspaceID: t.workspaceID,
		AgentID:     t.agentID,
		SessionID:   t.sessionID,
		Items:       items,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameTodoWrite, err)
	}
	return encodeTodoResult(NameTodoWrite, stored)
}

// todoReadTool returns the calling session's current todo list and revision
// (adopt-assistant-ui-elements D6): the full plan behind the one-line
// open-items summary the context carries. Read-only, no side effects.
type todoReadTool struct {
	todos       store.TodoStore
	workspaceID string
	agentID     string
	sessionID   string
}

// NewTodoRead constructs the read tool bound to the executing run's identity.
func NewTodoRead(todos store.TodoStore, workspaceID, agentID, sessionID string) (tool.BaseTool, error) {
	return &todoReadTool{
		todos:       todos,
		workspaceID: workspaceID,
		agentID:     agentID,
		sessionID:   sessionID,
	}, nil
}

// Info returns the tool schema: no arguments — the session identity is fixed
// at construction.
func (t *todoReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameTodoRead,
		Desc: "Read your todo list for this session: the full current item list with statuses and the revision counter. " +
			"Use it to re-ground your plan — for example after your context was compacted, or whenever the injected open-items summary is not enough. " +
			"Read-only: it changes nothing.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// InvokableRun satisfies tool.InvokableTool.
func (t *todoReadTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	stored, err := t.todos.GetBySession(ctx, t.workspaceID, t.agentID, t.sessionID)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameTodoRead, err)
	}
	return encodeTodoResult(NameTodoRead, stored)
}

// encodeTodoResult marshals the shared {items, revision} echo.
func encodeTodoResult(name string, list *store.TodoList) (string, error) {
	result := todoWriteResult{Items: []todoResultItem{}, Revision: list.Revision}
	for _, item := range list.Items {
		result.Items = append(result.Items, todoResultItem{
			Key:    item.ItemKey,
			Text:   item.ItemText,
			Status: string(item.Status),
			Reason: item.Reason,
		})
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("%s: encode result: %w", name, err)
	}
	return string(encoded), nil
}

// strictDecodeArgs decodes one tool call's arguments rejecting unknown
// fields — the strict-validation contract the generative-UI echo tools and
// the todo tools share (adopt-assistant-ui-elements D3): a schema violation
// is an error the model reads and corrects, never silently dropped input.
func strictDecodeArgs(name, argumentsInJSON string, v any) error {
	dec := json.NewDecoder(strings.NewReader(argumentsInJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
