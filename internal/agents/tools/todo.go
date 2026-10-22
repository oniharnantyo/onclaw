// Package tools holds the agent-facing built-in tools that live outside the
// eino filesystem middleware and outside the jailed backend package. Today
// that is the todo plan pair (todo_write/todo_read), which persists the
// agent's task plan as a document inside the agent's workspace so the plan
// survives across turns. Tools here implement the eino tool interfaces
// directly and take the agent's workspace root at construction; wiring into
// the tool catalog happens in the runtime composition, not here.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Todo tool names: dotted registry names so hooks can target them by exact
// name in matchers.
const (
	NameTodoWrite = "todo_write"
	NameTodoRead  = "todo_read"
)

// todoFileName is the plan document inside the agent workspace: a dot-file
// so it never masquerades as one of the workspace's documents.
const todoFileName = ".todo.json"

// The only statuses a plan item may carry.
const (
	todoStatusPending = "pending"
	todoStatusActive  = "active"
	todoStatusDone    = "done"
	todoStatusFailed  = "failed"
)

// validTodoStatus reports whether status is one of the four allowed values.
func validTodoStatus(status string) bool {
	switch status {
	case todoStatusPending, todoStatusActive, todoStatusDone, todoStatusFailed:
		return true
	}
	return false
}

// todoItem is one plan item, both on disk and in the tools' result echo.
type todoItem struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// todoEcho is the full plan state: the current items (list order) and the
// revision counter bumped by every successful write.
type todoEcho struct {
	Items    []todoItem `json:"items"`
	Revision int64      `json:"revision"`
}

// todoWorkspaceRoot validates and canonicalizes the workspace root the tools
// persist into: empty roots fail, a non-directory occupying the root fails,
// a missing root is created (self-heal, mirroring the fs jail's delete tool),
// and symlinks resolve to the canonical form so the tools and the jail agree
// on the same on-disk directory.
func todoWorkspaceRoot(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("workspace root cannot be empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", abs)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks in workspace root: %w", err)
	}
	return strings.TrimSuffix(resolved, string(filepath.Separator)), nil
}

// loadTodoFile reads the plan from the workspace. An absent file is the
// normal fresh state: an empty plan at revision 0. Every other read or
// decode failure is returned.
func loadTodoFile(root string) (todoEcho, error) {
	raw, err := os.ReadFile(filepath.Join(root, todoFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return todoEcho{Items: []todoItem{}}, nil
	}
	if err != nil {
		return todoEcho{}, fmt.Errorf("read plan: %w", err)
	}
	var plan todoEcho
	if err := json.Unmarshal(raw, &plan); err != nil {
		return todoEcho{}, fmt.Errorf("decode plan: %w", err)
	}
	if plan.Items == nil {
		plan.Items = []todoItem{}
	}
	return plan, nil
}

// saveTodoFile writes the plan into the workspace. The write is whole-file:
// a rejected call never reaches it, so stored state only ever changes
// through a validated list.
func saveTodoFile(root string, plan todoEcho) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, todoFileName), raw, 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

// todoWriteTool replaces the agent's todo list (the workspace-scoped plan).
// Scoping is structural: the tool is constructed with the executing agent's
// workspace root, so no argument can address another agent's plan. It
// performs no network or shell effects.
type todoWriteTool struct {
	workspaceRoot string
}

// NewTodoWrite constructs the write tool bound to one agent's workspace root.
func NewTodoWrite(workspaceRoot string) (tool.BaseTool, error) {
	root, err := todoWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", NameTodoWrite, err)
	}
	return &todoWriteTool{workspaceRoot: root}, nil
}

// Info returns the tool schema surfaced to agentic models. The description
// carries the rewrite contract: the call's item list IS the whole list.
func (t *todoWriteTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameTodoWrite,
		Desc: "Replace your todo list with the full item list passed in — the call is the whole list, not a delta. " +
			"The plan persists in your workspace across turns. " +
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
				// Gemini's GenerateContent rejects array parameters without a
				// declared element schema ("properties[items].items: missing
				// field"), which 400s every agent turn on Gemini-family
				// endpoints — the element shape must be declared, not just
				// described in prose.
				ElemInfo: &schema.ParameterInfo{
					Type: schema.Object,
					Desc: `One todo item.`,
					SubParams: map[string]*schema.ParameterInfo{
						"key": {
							Type:     schema.String,
							Required: true,
							Desc:     "Stable item key. Reuse a key to update that item in place; a key absent from the call deletes that item.",
						},
						"text": {
							Type:     schema.String,
							Required: true,
							Desc:     "Short display text for the item.",
						},
						"status": {
							Type:     schema.String,
							Required: true,
							Enum:     []string{todoStatusPending, todoStatusActive, todoStatusDone, todoStatusFailed},
							Desc:     `Exactly "pending", "active", "done", or "failed".`,
						},
						"reason": {
							Type: schema.String,
							Desc: "The failure explanation, for failed items only.",
						},
					},
				},
			},
		}),
	}, nil
}

// todoWriteArgs is the deserialized tool-call argument shape.
type todoWriteArgs struct {
	Items []todoItem `json:"items"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues. Validation runs BEFORE the plan file is
// touched, so a rejected list never mutates stored state.
func (t *todoWriteTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args todoWriteArgs
	if err := strictDecodeTodoArgs(NameTodoWrite, argumentsInJSON, &args); err != nil {
		return "", err
	}

	items := make([]todoItem, 0, len(args.Items))
	for i, item := range args.Items {
		if strings.TrimSpace(item.Key) == "" {
			return "", fmt.Errorf("%s: item %d: key is required", NameTodoWrite, i+1)
		}
		if strings.TrimSpace(item.Text) == "" {
			return "", fmt.Errorf("%s: item %q: text is required", NameTodoWrite, item.Key)
		}
		if !validTodoStatus(item.Status) {
			return "", fmt.Errorf(`%s: item %q: status must be one of "pending", "active", "done", "failed", got %q`, NameTodoWrite, item.Key, item.Status)
		}
		items = append(items, todoItem{
			Key:    item.Key,
			Text:   item.Text,
			Status: item.Status,
			Reason: item.Reason,
		})
	}
	// Duplicate keys cannot satisfy per-item uniqueness — name the first one
	// here so the model can correct the list directly.
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, dup := seen[item.Key]; dup {
			return "", fmt.Errorf("%s: duplicate item key %q", NameTodoWrite, item.Key)
		}
		seen[item.Key] = struct{}{}
	}

	current, err := loadTodoFile(t.workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameTodoWrite, err)
	}
	plan := todoEcho{Items: items, Revision: current.Revision + 1}
	if err := saveTodoFile(t.workspaceRoot, plan); err != nil {
		return "", fmt.Errorf("%s: %w", NameTodoWrite, err)
	}
	return encodeTodoResult(NameTodoWrite, plan)
}

// todoReadTool returns the agent's current todo list and revision: the full
// plan. Read-only, no side effects.
type todoReadTool struct {
	workspaceRoot string
}

// NewTodoRead constructs the read tool bound to one agent's workspace root.
func NewTodoRead(workspaceRoot string) (tool.BaseTool, error) {
	root, err := todoWorkspaceRoot(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", NameTodoRead, err)
	}
	return &todoReadTool{workspaceRoot: root}, nil
}

// Info returns the tool schema: no arguments — the workspace is fixed at
// construction.
func (t *todoReadTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameTodoRead,
		Desc: "Read your todo list: the full current item list with statuses and the revision counter. " +
			"Use it to re-ground your plan — for example after your context was compacted. " +
			"Read-only: it changes nothing.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

// InvokableRun satisfies tool.InvokableTool.
func (t *todoReadTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	plan, err := loadTodoFile(t.workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("%s: %w", NameTodoRead, err)
	}
	return encodeTodoResult(NameTodoRead, plan)
}

// encodeTodoResult marshals the shared {items, revision} echo. Items is
// always a list (never null) so model-facing JSON stays shape-stable.
func encodeTodoResult(name string, plan todoEcho) (string, error) {
	if plan.Items == nil {
		plan.Items = []todoItem{}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("%s: encode result: %w", name, err)
	}
	return string(encoded), nil
}

// strictDecodeTodoArgs decodes one todo tool call's arguments rejecting
// unknown fields — the strict-validation contract: a schema violation is an
// error the model reads and corrects, never silently dropped input.
func strictDecodeTodoArgs(name, argumentsInJSON string, v any) error {
	dec := json.NewDecoder(strings.NewReader(argumentsInJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
