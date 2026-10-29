package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/oniharnantyo/buddies/internal/agents/hooks"
	"github.com/oniharnantyo/buddies/internal/agents/subagents"
	"github.com/oniharnantyo/buddies/internal/memory"
	"github.com/oniharnantyo/buddies/internal/store"
)

// The memory_search tool (add-agent-memory tasks.md 5.2, spec agent-memory
// "Memory search tool" and "Recall traces and origin gate"): a lexical search
// over the session agent's workspace memory — fact notes, daily notes, and the
// index — returning ranked hits, with NO model call anywhere on the path
// (design D5). Every invocation records a recall trace carrying the calling
// turn's origin (design D6/D12): the traces are the consolidation sweep's sole
// signal source, and only user-origin ones count — so a scheduled run's and a
// child turn's searches are stored but can never reinforce memory by looping
// over it.

// memorySearchToolDescription is the model-facing description of the
// memory_search tool; the settings surfaces render the catalog entry's
// shorter one (builtin_tools.go).
const memorySearchToolDescription = "Search this agent's long-term memory — saved fact notes about the user's preferences, past decisions, corrections, and earlier work — and get the matching notes back ranked by match strength, each with an excerpt. " +
	"Lexical match: query with the words or identifiers (memory_search, CONFIG_PATH) the memory would use, and refine the query rather than repeating it. " +
	"Read the full note with the filesystem tools when an excerpt is not enough. Missing memory is a normal empty result."

// memoryTurnContextFunc resolves the turn context of one tool call: the
// session and turn the call belongs to and the trace origin it carries. The
// manager implements it (memoryTurnContext below); tests substitute fakes.
type memoryTurnContextFunc func(ctx context.Context) (sessionID, turnID, origin string)

// newMemorySearchTool builds the memory_search invokable tool over the
// session agent's workspace memory directory. traces records the recall
// traces; turnCtx resolves the calling turn's identity and origin.
func newMemorySearchTool(workspaceDir, agentID string, traces store.MemoryStore, turnCtx memoryTurnContextFunc) (tool.BaseTool, error) {
	if workspaceDir == "" {
		return nil, errors.New("memory_search: no workspace configured")
	}
	if traces == nil {
		return nil, errors.New("memory_search: no memory store configured")
	}
	if turnCtx == nil {
		return nil, errors.New("memory_search: no turn context source configured")
	}
	st := &memorySearchTool{
		layout:  memory.NewLayout(workspaceDir),
		agentID: agentID,
		traces:  traces,
		turnCtx: turnCtx,
	}
	return utils.InferTool(ToolMemorySearch, memorySearchToolDescription, st.run)
}

// memorySearchTool is the memory_search tool's closed-over dependencies: the
// workspace-rooted memory layout it searches, the agent its traces belong to,
// the trace store, and the calling turn's context source.
type memorySearchTool struct {
	layout  memory.Layout
	agentID string
	traces  store.MemoryStore
	turnCtx memoryTurnContextFunc
}

// MemorySearchArgs is the tool's whole input: the query and an optional
// result limit (the default applies when zero or negative).
type MemorySearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// MemorySearchHit is one ranked result: the note's name, description,
// workspace-relative path, match score, and a matched excerpt (spec
// agent-memory "Memory search tool").
type MemorySearchHit struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Path        string  `json:"path"`
	Score       float64 `json:"score"`
	Excerpt     string  `json:"excerpt"`
}

// MemorySearchOutput is the tool's whole output: the ranked results on
// success; ok=false with a readable error when the query was empty or the
// memory directory could not be read — a readable result, never a Go error,
// so the turn continues (spec agent-memory: a read failure is returned as a
// readable error result).
type MemorySearchOutput struct {
	OK      bool              `json:"ok"`
	Error   string            `json:"error,omitempty"`
	Results []MemorySearchHit `json:"results,omitempty"`
}

// run executes one memory_search call: search, record the recall trace, and
// return the ranked hits.
func (t *memorySearchTool) run(ctx context.Context, in MemorySearchArgs) (MemorySearchOutput, error) {
	query := in.Query
	if query == "" {
		return MemorySearchOutput{OK: false,
			Error: "provide a query: the words or identifiers the memory note would use",
		}, nil
	}

	ix := memory.NewIndex(t.layout)
	// A fresh Index per call is deliberate: the mtime-rebuild policy makes the
	// rebuild decision per call, so a cached instance would save nothing —
	// the sweep owns the long-lived per-agent index.
	hits, err := ix.Search(ctx, query, in.Limit)
	if err != nil {
		// Unreadable memory is a readable rejection, not a turn failure (spec
		// agent-memory "Memory search tool").
		return MemorySearchOutput{OK: false,
			Error: fmt.Sprintf("memory could not be read: %v", err),
		}, nil
	}

	t.recordTrace(ctx, query, hits)

	results := make([]MemorySearchHit, 0, len(hits))
	for _, h := range hits {
		results = append(results, MemorySearchHit{
			Name:        h.Name,
			Description: h.Description,
			Path:        h.Path,
			Score:       h.Score,
			Excerpt:     h.Excerpt,
		})
	}
	return MemorySearchOutput{OK: true, Results: results}, nil
}

// recordTrace appends the call's recall trace (design D6). The trace is the
// sweep's signal source, but a trace-write failure must not fail a search
// that already succeeded: it logs, and the turn continues untraced.
func (t *memorySearchTool) recordTrace(ctx context.Context, query string, hits []memory.SearchHit) {
	sessionID, turnID, origin := t.turnCtx(ctx)
	traceHits := make([]store.MemoryTraceHit, 0, len(hits))
	for _, h := range hits {
		traceHits = append(traceHits, store.MemoryTraceHit{Path: h.Path, Score: h.Score})
	}
	if _, err := t.traces.AppendTrace(ctx, store.MemoryTrace{
		AgentID:   t.agentID,
		SessionID: sessionID,
		TurnID:    turnID,
		Origin:    origin,
		Query:     query,
		Hits:      traceHits,
	}); err != nil {
		slog.Warn("memory_search: record recall trace", "agent", t.agentID, "error", err)
	}
}

// memoryTurnContext resolves the calling turn's identity and origin for the
// trace (add-agent-memory design D12): a child turn's delegation marker wins
// — its traces carry the subagent origin whatever started the parent run;
// otherwise the session's running turn supplies the dispatch origin (the same
// source the hook events stamp, design D6, an empty origin reading as user)
// mapped onto the trace vocabulary; a call with no session or no running turn
// resolves to other.
func (m *manager) memoryTurnContext(ctx context.Context) (sessionID, turnID, origin string) {
	// A child turn's delegation marker wins: its traces carry the subagent
	// origin whatever started the parent run. The detached background lane
	// drops the eino runner session value, so the session rides the marker
	// itself (stamped at the delegation call, where it is still readable).
	if subagents.IsChildTurn(ctx) {
		sessionID := subagents.ChildTurnSessionID(ctx)
		if sessionID == "" {
			// A foreground delegation keeps the value in the context.
			sessionID, _ = adk.RunnerSessionID(ctx)
		}
		return sessionID, m.runningTurnID(sessionID), store.MemoryOriginSubagent
	}
	sessionID, ok := adk.RunnerSessionID(ctx)
	if !ok {
		return "", "", store.MemoryOriginOther
	}
	m.mu.Lock()
	rt, inFlight := m.running[sessionID]
	m.mu.Unlock()
	if !inFlight {
		// A detached call with no running turn on record — a background child
		// that outlived its parent's turn, or a foreign context.
		return sessionID, "", store.MemoryOriginOther
	}
	return sessionID, rt.turn.id, memoryTraceOrigin(rt.origin)
}

// runningTurnID returns the session's in-flight turn id, or "" when none —
// the best coordinate a child trace can carry for the turn that dispatched it.
func (m *manager) runningTurnID(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.running[sessionID]; ok && rt != nil {
		return rt.turn.id
	}
	return ""
}

// memoryTraceOrigin maps a dispatch origin (hooks.Origin*, design D6) onto
// the trace origin vocabulary (store.MemoryOrigin*, design D12): the two
// share the user and scheduler values; anything else — a future trigger
// surface's origin, or an unknown one — is other.
func memoryTraceOrigin(runOrigin string) string {
	switch runOrigin {
	case "":
		return store.MemoryOriginUser // an empty dispatch origin reads as user
	case hooks.OriginUser:
		return store.MemoryOriginUser
	case hooks.OriginScheduler:
		return store.MemoryOriginScheduler
	default:
		return store.MemoryOriginOther
	}
}
