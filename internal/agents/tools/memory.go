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
	"github.com/oniharnantyo/onclaw/internal/memory"
	"github.com/oniharnantyo/onclaw/internal/store"
)

// NameMemory is the dotted capability name registered in the tool registry.
const NameMemory = "memory"

// NameMemorySearch is the read-only memory search tool's registry name
// (integrate-agent-zero-memory 4.3): the only memory tool the model gets over
// the extracted stores — zero writes, zero model calls.
const NameMemorySearch = "memory.search"

// Memory path grammar (design.md D4): two exact shared-scope document names.
const (
	memoryPathUser      = "USER.md"
	memoryPathWorkspace = "WORKSPACE.md"
	// memoryEmptyMarker is returned by read when nothing is stored yet.
	memoryEmptyMarker = "(empty — no memory stored yet)"
)

// memoryTool scoping is structural (design.md D5): the tool is constructed with
// the executing run's workspace and user identity, so no argument can address
// another principal's document.
type memoryTool struct {
	memories    store.MemoryStore
	workspaceID string
	userID      string
}

// NewMemory constructs the memory document tool bound to the executing run's
// identity: user memory is per (workspace, user), workspace memory is shared
// per workspace. The two-document grammar scopes only by workspace and user.
func NewMemory(memories store.MemoryStore, workspaceID, userID string) (tool.BaseTool, error) {
	return &memoryTool{
		memories:    memories,
		workspaceID: workspaceID,
		userID:      userID,
	}, nil
}

// Info returns the tool schema surfaced to agentic models. The description is
// the only instruction surface guaranteed to reach every agent regardless of
// AGENTS.md vintage (design.md D11) — it carries the full contract.
func (t *memoryTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameMemory,
		Desc: "Persistent memory across conversations, in two documents addressed by path. " +
			"USER.md — memory about the current user (one document per workspace and user). " +
			"WORKSPACE.md — shared team memory (one document per workspace). " +
			"Actions: read returns the document's stored content, or an explicit empty marker when nothing is stored yet. " +
			"append adds content to the end of the document — append requires content and is the only write: there is no overwrite or delete, corrections are appended. " +
			"Never re-store information already visible in your context: workspace and user metadata is injected into your instructions free every turn. " +
			"Keep entries short and factual.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"path": {
				Type:     schema.String,
				Desc:     "The memory document: USER.md or WORKSPACE.md.",
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

// memoryRef is a resolved memory document: one of the two scopes plus the
// canonical document name used in confirmations.
type memoryRef struct {
	scope    string // "user" | "workspace"
	document string
}

const (
	memoryScopeUser      = "user"
	memoryScopeWorkspace = "workspace"
)

// resolveMemoryPath applies the D4 path grammar. Anything that is not one of
// the two accepted forms — including the retired daily paths MEMORY-TODAY.md
// and MEMORY-DD-MM-YYYY.md — is rejected with an error naming them.
func resolveMemoryPath(path string) (memoryRef, error) {
	switch path {
	case memoryPathUser:
		return memoryRef{scope: memoryScopeUser, document: memoryPathUser}, nil
	case memoryPathWorkspace:
		return memoryRef{scope: memoryScopeWorkspace, document: memoryPathWorkspace}, nil
	}
	return memoryRef{}, fmt.Errorf(
		"unknown memory path %q — accepted forms are USER.md and WORKSPACE.md", path)
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
	ref, err := resolveMemoryPath(args.Path)
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
	if ref.scope == memoryScopeWorkspace {
		return t.memories.WorkspaceMemory(ctx, t.workspaceID)
	}
	return t.memories.UserMemory(ctx, t.workspaceID, t.userID)
}

// appendStore adds content to the resolved scope's document.
func (t *memoryTool) appendStore(ctx context.Context, ref memoryRef, content string) error {
	if ref.scope == memoryScopeWorkspace {
		return t.memories.AppendWorkspaceMemory(ctx, t.workspaceID, content)
	}
	return t.memories.AppendUserMemory(ctx, t.workspaceID, t.userID, content)
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

// ---------------------------------------------------------------------------
// memory.search (integrate-agent-zero-memory 4.3, spec agent-memory-retrieval)
// ---------------------------------------------------------------------------

// memorySearchDefaultLimit / memorySearchMaxLimit bound one search call.
const (
	memorySearchDefaultLimit = 8
	memorySearchMaxLimit     = 25
)

// memorySearchEmptyHint is the structured empty result's explicit note: the
// model must read a zero result as "nothing is recorded", never as license to
// recall from elsewhere (the abstention contract).
const memorySearchEmptyHint = "Nothing is recorded in memory for this query."

// memorySearchTool is the read-only search over the extracted stores: notes
// and events, hybrid lexical, identity-bound structurally. The caller
// identity is fixed at construction from the run's ToolContext — the query
// arguments carry no identity fields and can never widen the visible set
// (shared + own-user + serving-agent rows, computed in the store query).
// The tool writes nothing and calls no model.
type memorySearchTool struct {
	searcher *memory.Searcher
	caller   memory.Caller
}

// NewMemorySearch constructs the search tool bound to the executing run's
// identity. The searcher is composition-root wiring (never nil).
func NewMemorySearch(searcher *memory.Searcher, workspaceID, userID, agentID string) (tool.BaseTool, error) {
	return &memorySearchTool{
		searcher: searcher,
		caller: memory.Caller{
			WorkspaceID: workspaceID,
			UserID:      userID,
			AgentID:     agentID,
		},
	}, nil
}

// Info returns the tool schema. The description carries the read-only,
// provenance-bearing contract; the arguments are filters only — no identity,
// no write surface.
func (t *memorySearchTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: NameMemorySearch,
		Desc: "Search the workspace's extracted long-term memory: curated facts (notes) and episodic summaries (events), filtered to what you are allowed to see. " +
			"Read-only: it never stores anything and has no side effects. " +
			"Results carry provenance — origin, timestamps, the source session event id, and visibility — cite the source event id for anything you use. " +
			"An empty result means nothing is recorded: say so rather than inventing recalled content.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "Free-text search over note content and event descriptions/outcomes. Required.",
				Required: true,
			},
			"from": {
				Type: schema.String,
				Desc: "Optional RFC3339 lower bound: only rows whose event_time (when the fact was true) is at or after this instant.",
			},
			"until": {
				Type: schema.String,
				Desc: "Optional RFC3339 upper bound (exclusive) on event_time.",
			},
			"visibility": {
				Type: schema.String,
				Desc: `Optional filter narrowing to one visible tier: "shared", "user", or "agent". It can only narrow what you may already see.`,
			},
			"topic": {
				Type: schema.String,
				Desc: "Optional topic label to filter notes by.",
			},
			"limit": {
				Type: schema.Integer,
				Desc: "Optional maximum combined results (default 8, capped at 25).",
			},
		}),
	}, nil
}

// memorySearchArgs is the deserialized argument shape. It deliberately
// carries no identity fields: the caller is the only scope input (D8).
type memorySearchArgs struct {
	Query      string `json:"query"`
	From       string `json:"from"`
	Until      string `json:"until"`
	Visibility string `json:"visibility"`
	Topic      string `json:"topic"`
	Limit      *int   `json:"limit"`
}

// memorySearchNote is one result row's model-facing projection: content plus
// the provenance tuple (origin, both timestamps, evidence pointer,
// visibility).
type memorySearchNote struct {
	ID            string    `json:"id"`
	Content       string    `json:"content"`
	Visibility    string    `json:"visibility"`
	Origin        string    `json:"origin"`
	EventTime     time.Time `json:"event_time"`
	LearnedAt     time.Time `json:"learned_at"`
	SourceEventID string    `json:"source_event_id"`
	Topic         string    `json:"topic,omitempty"`
}

// memorySearchEvent is the episodic equivalent.
type memorySearchEvent struct {
	ID            string    `json:"id"`
	Description   string    `json:"description"`
	Outcome       string    `json:"outcome"`
	Visibility    string    `json:"visibility"`
	Origin        string    `json:"origin"`
	EventTime     time.Time `json:"event_time"`
	LearnedAt     time.Time `json:"learned_at"`
	SourceEventID string    `json:"source_event_id"`
}

// memorySearchResult is the structured answer. When nothing matched, Hint
// states the empty result in words the model reads as "nothing is recorded".
type memorySearchResult struct {
	Notes  []memorySearchNote  `json:"notes"`
	Events []memorySearchEvent `json:"events"`
	Hint   string              `json:"hint,omitempty"`
}

// InvokableRun satisfies tool.InvokableTool. Failures return errors: the
// runtime's tool-error middleware converts them into JSON error results the
// model reads, so the run continues.
func (t *memorySearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var args memorySearchArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("memory.search: %w", err)
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return "", errors.New("memory.search: query is required")
	}

	window, err := memorySearchTimeWindow(args.From, args.Until)
	if err != nil {
		return "", err
	}
	visibility := domain.MemoryVisibility(strings.ToLower(strings.TrimSpace(args.Visibility)))
	if visibility != "" && !domain.ValidMemoryVisibility(visibility) {
		return "", fmt.Errorf(`memory.search: visibility must be "shared", "user", or "agent", got %q`, args.Visibility)
	}
	limit := memorySearchDefaultLimit
	if args.Limit != nil {
		limit = *args.Limit
	}
	if limit <= 0 {
		limit = memorySearchDefaultLimit
	}
	if limit > memorySearchMaxLimit {
		limit = memorySearchMaxLimit
	}

	result, err := t.searcher.Search(ctx, t.caller, memory.Query{
		Text:             query,
		TimeWindow:       window,
		VisibilityFilter: visibility,
		Topic:            strings.TrimSpace(args.Topic),
		Limit:            limit,
	})
	if err != nil {
		return "", fmt.Errorf("memory.search: %w", err)
	}

	out := memorySearchResult{Notes: []memorySearchNote{}, Events: []memorySearchEvent{}}
	for _, n := range result.Notes {
		topic := ""
		if n.Topic != nil {
			topic = *n.Topic
		}
		out.Notes = append(out.Notes, memorySearchNote{
			ID:            n.ID,
			Content:       n.Content,
			Visibility:    string(n.Visibility),
			Origin:        string(n.Origin),
			EventTime:     n.EventTime,
			LearnedAt:     n.LearnedAt,
			SourceEventID: n.SourceEventID,
			Topic:         topic,
		})
	}
	for _, e := range result.Events {
		out.Events = append(out.Events, memorySearchEvent{
			ID:            e.ID,
			Description:   e.Description,
			Outcome:       e.Outcome,
			Visibility:    string(e.Visibility),
			Origin:        string(e.Origin),
			EventTime:     e.EventTime,
			LearnedAt:     e.LearnedAt,
			SourceEventID: e.SourceEventID,
		})
	}
	if len(out.Notes) == 0 && len(out.Events) == 0 {
		out.Hint = memorySearchEmptyHint
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("memory.search: encode result: %w", err)
	}
	return string(encoded), nil
}

// memorySearchTimeWindow parses the optional RFC3339 bounds; empty strings
// are open ends. A malformed bound is a structured error naming the format.
func memorySearchTimeWindow(from, until string) (*store.MemoryTimeWindow, error) {
	if strings.TrimSpace(from) == "" && strings.TrimSpace(until) == "" {
		return nil, nil
	}
	window := &store.MemoryTimeWindow{}
	if strings.TrimSpace(from) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(from))
		if err != nil {
			return nil, fmt.Errorf("memory.search: from must be an RFC3339 timestamp, got %q", from)
		}
		window.From = t
	}
	if strings.TrimSpace(until) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(until))
		if err != nil {
			return nil, fmt.Errorf("memory.search: until must be an RFC3339 timestamp, got %q", until)
		}
		window.To = t
	}
	return window, nil
}
