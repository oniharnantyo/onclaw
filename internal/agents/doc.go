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
// # Package Architecture & Subpackages
//
// The agent runtime is modularized into specialized subpackages:
//
//   - internal/agents (root): the service surface (manager, manager_impl,
//     registry, catalog, builtin_tools, connectors, prompts, types).
//   - internal/agents/compose: ADK agent composition, middleware pipeline
//     (patchtoolcalls -> reduction -> summarization -> skill -> filesystem ->
//     plantask -> hooks -> subagent -> backgroundtask -> tool-error-result),
//     tool gating (ResolvedTools), and capability resolver.
//   - internal/agents/run: turn execution engine (Runner, RunningTurn, Turn,
//     DrainValues, subscriptions, and turn loop lifecycle).
//   - internal/agents/compaction: working context compaction at 0.8 threshold
//     into persisted SQLite checkpoints, and the in-run summarization safety
//     net middleware (~0.9 context limit).
//   - internal/agents/subagents: delegated subagent adapter over eino
//     subagent and backgroundtask middlewares, run-scoped task ID sequencing,
//     and task activity bridging.
//   - internal/agents/backend: filesystem and shell jails (reduction, plantask,
//     filesystem backends).
//   - internal/agents/tools: web tools (browser, web_fetch, web_search).
//   - internal/agents/mcp: Model Context Protocol (MCP) client manager and tool adapter.
//   - internal/agents/hooks: deterministic pre- and post-tool lifecycle dispatchers.
package agents
