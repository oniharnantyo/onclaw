## Why

onclaw caps tool **downloads** at `max_bytes` (default 1 MB) but injects the full result inline into
the model context. On the default 64k-token budget (`max_context_tokens: 64000`) running on ~2 GB
RAM / 8 GB storage, a single large `web_fetch`, `browser_snapshot`, or `session_search` (~1 MB ≈
250k tokens) blows the context ~4× over and destabilizes the agent. `max_bytes` protects RAM/transfer;
nothing protects **context tokens**. Separately, `kg_search` and `memory` both register the `"Memory"`
tool-group config schema, so one clobbers the other depending on `init()` filename order — a latent
config bug that would also hide any new Memory config field.

## What Changes

- **File-reference spill mode (generic decorator):** a `WrapFileSpill` decorator (sibling to
  `WrapRedacted`) writes any tool result exceeding a per-group `spill_threshold_bytes` to a
  session-scoped file and returns a small, stable envelope (path + size + preview + a `read_file`
  recovery hint) instead of the blob. Applied to every factory-registered tool.
- **Session-scoped artifacts:** spilled files live at
  `.onclaw/workspace/<agent>/sessions/<sid>/tool_results/<tool>_<timestamp>_<title>.md` under the
  resolved workspace; the envelope returns a workspace-relative path so `read_file` consumes it
  directly. Kept for the session lifetime.
- **Per-group threshold:** `spill_threshold_bytes` added to the Web, Memory, and Browser tool-group
  config schemas, resolved per-call (hot-reload-safe). Decoupled from `max_bytes`.
- **Secrets redacted before spill:** decorator ordering `WrapFileSpill(WrapRedacted(tool))` ensures
  the spilled file and preview never contain raw secrets.
- **SessionID threading:** `Scope` gains `SessionID` (derived from `conversationID`, like the hooks
  `SessionState`) so the spill path can namespace per session.
- **Config collision fix:** `kg_search`'s standalone `"Memory"` config registration is removed; its
  `max_depth` knob folds into the shared Memory schema so each tool-group category has exactly one
  schema owner.
- **Summarization interop (durable pointer across compaction):** a spilled result is the
  live-context view of an on-disk artifact, so the summarizer-input scrub preserves the **spill
  file path** verbatim when condensing it (dropping the preview/body). This extends the compaction
  layer's durable-pointer preservation so large results offloaded to files remain recallable by path
  after compaction instead of being reduced to a generic `[cleared <tool>]` stub that drops the path.
- **Filesystem search caps:** `grep` and `glob` cap their results at the onclaw-owned backend
  (truncation + a recovery hint), protecting context for the two FS tools that do not self-bound.
  This is cap-at-source, distinct from the factory spill decorator — FS tools are injected by the
  Eino filesystem middleware and bypass `Builtin()`, so they are not wrapped by `WrapFileSpill`.

- **Image artifact persistence:** `browser_screenshot` writes the raw PNG to a session-scoped `.png`
  under `tool_results` and returns a path-reference envelope instead of a large base64 data URL —
  binary handling in-tool (not the text spill decorator), reusing `spillPath` and `Scope.SessionID`.

## Impact

- **Specs:** `agent-tools` gains requirements for spill behavior, per-group threshold, singly-owned
  config categories, session-scoped artifacts, and bounded filesystem search. `conversation-history`
  gains a requirement that summarization preserves spilled-result file paths.
- **Code:** `internal/agent/tools/{tools,registry,spill,spill_test,fsbackend}.go`;
  `internal/agent/agent.go` (`Scope.SessionID`); `internal/agent/tools/{web/register,memory,browser/register}.go`
  (schemas); `internal/agent/tools/kg_search.go` (drop the clobbering `RegisterConfig`, read
  `max_depth` from Memory); `internal/agent/summarization_scrub.go` (promote spill-path-bearing
  results to the path-preserving stub class).
- **Compatibility:** additive. The new config field ships with safe defaults; tools that already
  self-bound (`execute` 32 KB cap; `read_file` offset/limit) remain so, and `grep`/`glob` gain result
  caps. MCP tools and the FS-middleware tool surface are otherwise unchanged (out of scope for v1).
  Existing persisted config rows gain the default on read. The summarizer change only affects what is
  condensed for the summary model, not the live replay prefix.