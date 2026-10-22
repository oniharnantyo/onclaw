// Package agents is the agent runtime: it executes chat turns for stored
// agent configurations on the eino framework — session context assembly,
// token streaming, context-window accounting, compaction, and the provider
// registry.
//
// # Package boundary
//
// Eino types (schema.Message, model.BaseChatModel, connector configs, stream
// readers) MUST NOT leak out of this package. Everything crossing the
// boundary is plain Go:
//
//   - Manager, Turn, Subscription, ContextUsage — the public API surface;
//   - sessions.Event / sessions.Session / sessions.Checkpoint — the event
//     vocabulary, owned by internal/sessions and JSON-serializable;
//   - store.Store / store.Agent — persistence types from internal/store.
//
// The eino chat-model abstraction (model.BaseChatModel) and the per-provider
// connectors live behind the unexported modelRegistry interface
// (registry.go/connectors.go); turn execution consumes streamed chunks
// through it and republishes them as sessions events (runner.go). This keeps
// eino upgrades local to the package and lets the services wave bind the
// Manager without any eino dependency.
//
// # Layout
//
//   - manager.go: the Manager port (session lifecycle, Send, Subscribe,
//     ContextUsage) and its constructor wiring.
//   - manager_impl.go: the concrete manager: session lifecycle, the
//     per-session busy guard, and subscription fan-out.
//   - runner.go: one turn's execution: the ADK event drain (tool loop →
//     sessions events), context assembly, streaming deltas, event
//     persistence.
//   - compose.go: the compose seam (design D1): Config/Compose building the
//     ADK agent (tool loop + ordered capability middlewares) per turn; the
//     ordered handler list is the extension seam later waves hang their
//     middlewares on.
//   - toolerror.go: the tool-error-result middleware: tool failures become
//     error results the model reads instead of run-killing errors, and the
//     per-turn failure registry the drain consults for terminal records.
//   - gate.go: the tool gate: resolve (catalog × global enable × agent
//     allowlist) → the turn's tool surface handed to compose.
//   - mcp.go: the MCP seam (design D7): the opt-in policy (enabled server +
//     agent opt-in, bypassing the tool allowlist) and the pass-scoped
//     mcp__<server>__<tool> renaming, merged into the turn's tool surface.
//   - builtin_tools.go: the built-in tool catalog and the workspace-free
//     tool constructors.
//   - websearch_source.go: the tool_settings-backed adapter satisfying the
//     tools package's SearchConfigSource seam.
//   - turn.go: the Turn handle returned by Send (async per design D7).
//   - subscription.go: per-session live event subscriptions; drop-new
//     semantics so a slow consumer never stalls a turn — durability lives in
//     the store, and reconnecting consumers replay from it.
//   - registry.go: the provider registry: builds the eino model for an
//     agent's configured provider, with typed errors for unconfigured
//     providers.
//   - connectors.go: provider type to eino connector mapping (Anthropic
//     wired end-to-end; OpenAI and Gemini registered).
//   - catalog.go: the built-in model catalog (context windows per model id).
//   - context.go: context-window accounting from persisted usage.
//   - compact.go: threshold compaction of the working context.
//
// # Subpackages
//
// The jailed working surface lives in two subpackages the composition wires
// in; both keep their own boundaries:
//
//   - backend: the filesystem jail (einofs.Backend), the jailed shell
//     (einofs.Shell), and the workspace-scoped delete_file tool.
//   - tools: the todo plan pair and the web tools (web_fetch, the web_search
//     provider chain), constructed per turn by the gate.
//   - mcp: the MCP client runtime (design D7): the stdio/streamable-HTTP/SSE
//     transport factory, the idle-reaped connection manager with
//     negative-cache retry and invalidate-on-write, the mcp__<server>__<tool>
//     namer, and the probe.
package agents
