package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/oniharnantyo/onclaw/internal/domain"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// NameMemory is the dotted capability name registered in the tool registry.
const NameMemory = "memory"

// Memory path grammar (design.md D4): exact shared-scope names, the TODAY
// alias resolved in the workspace timezone, and strictly parsed dated daily
// documents.
const (
	memoryPathUser      = "USER.md"
	memoryPathWorkspace = "WORKSPACE.md"
	memoryDailyPrefix   = "MEMORY-"
	memoryDailySuffix   = ".md"
	memoryDailyToday    = "MEMORY-TODAY.md"
	// memoryDailyLayout is the strict dd-mm-yyyy shape of the dated daily
	// documents; the dd-mm-yyyy format exists only at this tool boundary.
	memoryDailyLayout = "02-01-2006"
	// memoryEmptyMarker is returned by read when nothing is stored yet.
	memoryEmptyMarker = "(empty — no memory stored yet)"
)

// Memory scoping is structural (design.md D5): the tool is constructed with
// the executing run's workspace, agent, and user identity plus the
// workspace-local timezone, so no argument can address another principal.
type memoryTool struct {
	memories    store.MemoryStore
	workspaceID string
	agentID     string
	userID      string
	workspaceTZ *time.Location
}

// NewMemory constructs the memory built-in bound to the executing run's
// identity: user memory is per (workspace, user), workspace memory is shared
// per workspace, and daily memory is per (workspace, agent, date). The
// timezone resolves MEMORY-TODAY.md to the workspace-local date.
func NewMemory(memories store.MemoryStore, workspaceID, agentID, userID string, workspaceTZ *time.Location) (tool.BaseTool, error) {
	return &memoryTool{
		memories:    memories,
		workspaceID: workspaceID,
		agentID:     agentID,
		userID:      userID,
		workspaceTZ: workspaceTZ,
	}, nil
}

// Info returns the tool schema surfaced to agentic models. The description is
// the only instruction surface guaranteed to reach every agent regardless of
// AGENTS.md vintage (design.md D11) — it carries the full contract.
func (t *memoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameMemory,
		Desc: "Persistent memory across conversations, in three scopes addressed by path. " +
			"USER.md — memory about the current user (one document per workspace and user). " +
			"WORKSPACE.md — shared team memory (one document per workspace). " +
			"MEMORY-DD-MM-YYYY.md — your private log for that one day; MEMORY-TODAY.md is an alias that resolves to today's date document. " +
			"Actions: read returns the document's stored content, or an explicit empty marker when nothing is stored yet. " +
			"append adds content to the end of the document — append requires content and is the only write: there is no overwrite or delete, corrections are appended. " +
			"Never re-store information already visible in your context: workspace and user metadata is injected into your instructions free every turn. " +
			"Keep entries short and factual.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The memory document: USER.md, WORKSPACE.md, MEMORY-TODAY.md, or MEMORY-DD-MM-YYYY.md (dd-mm-yyyy).",
				Required: true,
			},
			"action": {
				Type:     schema.String,
				Desc:     `Either "read" or "append".`,
				Required: true,
			},
			"content": {
				Type: schema.String,
				Desc: "The text to add to the end of the document. Required when action is append; ignored for read.",
			},
		}),
	}, nil
}

// memoryArgs is the deserialized tool-call argument shape.
type memoryArgs struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Content string `json:"content"`
}

// memoryRef is a resolved memory document: one of the three scopes plus the
// canonical document name used in confirmations.
type memoryRef struct {
	scope    string // "user" | "workspace" | "daily"
	date     time.Time
	document string
}

const (
	memoryScopeUser      = "user"
	memoryScopeWorkspace = "workspace"
	memoryScopeDaily     = "daily"
)

// resolveMemoryPath applies the D4 path grammar. Anything that is not one of
// the four accepted forms is rejected with an error listing them.
func resolveMemoryPath(path string, tz *time.Location) (memoryRef, error) {
	switch path {
	case memoryPathUser:
		return memoryRef{scope: memoryScopeUser, document: memoryPathUser}, nil
	case memoryPathWorkspace:
		return memoryRef{scope: memoryScopeWorkspace, document: memoryPathWorkspace}, nil
	case memoryDailyToday:
		now := time.Now().In(tz)
		return memoryRef{
			scope:    memoryScopeDaily,
			date:     time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			document: memoryDailyPrefix + now.Format(memoryDailyLayout) + memoryDailySuffix,
		}, nil
	}
	if strings.HasPrefix(path, memoryDailyPrefix) && strings.HasSuffix(path, memoryDailySuffix) {
		stamp := strings.TrimSuffix(strings.TrimPrefix(path, memoryDailyPrefix), memoryDailySuffix)
		date, err := time.Parse(memoryDailyLayout, stamp)
		if err == nil {
			return memoryRef{
				scope:    memoryScopeDaily,
				date:     date,
				document: memoryDailyPrefix + date.Format(memoryDailyLayout) + memoryDailySuffix,
			}, nil
		}
	}
	return memoryRef{}, fmt.Errorf(
		"unknown memory path %q — accepted forms are USER.md, WORKSPACE.md, MEMORY-TODAY.md, or MEMORY-DD-MM-YYYY.md (dd-mm-yyyy, e.g. MEMORY-08-09-2026.md)", path)
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *memoryTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args memoryArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("memory: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", errors.New("memory: path is required")
	}
	ref, err := resolveMemoryPath(args.Path, t.workspaceTZ)
	if err != nil {
		return "", fmt.Errorf("memory: %w", err)
	}

	switch args.Action {
	case "read":
		return t.read(ctx, ref)
	case "append":
		return t.append(ctx, ref, args.Content)
	default:
		return "", errors.New(`memory: action must be "read" or "append"`)
	}
}

// get fetches the stored document for a resolved scope; a nil result means
// nothing is stored yet.
func (t *memoryTool) get(ctx context.Context, ref memoryRef) (*domain.Memory, error) {
	switch ref.scope {
	case memoryScopeUser:
		return t.memories.UserMemory(ctx, t.workspaceID, t.userID)
	case memoryScopeWorkspace:
		return t.memories.WorkspaceMemory(ctx, t.workspaceID)
	default:
		return t.memories.AgentDailyMemory(ctx, t.workspaceID, t.agentID, ref.date)
	}
}

// appendStore adds content to the resolved scope's document.
func (t *memoryTool) appendStore(ctx context.Context, ref memoryRef, content string) error {
	switch ref.scope {
	case memoryScopeUser:
		return t.memories.AppendUserMemory(ctx, t.workspaceID, t.userID, content)
	case memoryScopeWorkspace:
		return t.memories.AppendWorkspaceMemory(ctx, t.workspaceID, content)
	default:
		return t.memories.AppendAgentDailyMemory(ctx, t.workspaceID, t.agentID, ref.date, content)
	}
}

// read returns the stored content, or the explicit empty marker when nothing
// is stored.
func (t *memoryTool) read(ctx context.Context, ref memoryRef) (string, error) {
	mem, err := t.get(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("memory: read %s: %w", ref.document, err)
	}
	content := memoryEmptyMarker
	if mem != nil && mem.Content != "" {
		content = mem.Content
	}
	out, err := json.Marshal(map[string]string{"path": ref.document, "content": content})
	if err != nil {
		return "", fmt.Errorf("memory: encode result: %w", err)
	}
	return string(out), nil
}

// append pre-checks the size cap against the resulting document, then appends
// through the store's atomic append. A cap rejection (pre-check or the
// store's in-statement guard) leaves the stored memory unchanged.
func (t *memoryTool) append(ctx context.Context, ref memoryRef, content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errors.New("memory: content is required for the append action")
	}
	current, err := t.get(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("memory: read %s: %w", ref.document, err)
	}
	currentContent := ""
	if current != nil {
		currentContent = current.Content
	}
	if err := domain.ValidateMemoryAppend(currentContent, content); err != nil {
		return "", fmt.Errorf("memory: %w", err)
	}
	if err := t.appendStore(ctx, ref, content); err != nil {
		return "", fmt.Errorf("memory: %w", err)
	}
	confirmation := "Appended to " + ref.document
	out, err := json.Marshal(map[string]string{"path": ref.document, "result": confirmation})
	if err != nil {
		return "", fmt.Errorf("memory: encode result: %w", err)
	}
	return string(out), nil
}
