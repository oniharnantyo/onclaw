## Context

**Current State:** Factory tools (`web_*`, `browser_*`, `memory*`, `session_search`, `kg_search`) are
built in `tools.Builtin()` (`internal/agent/tools/registry.go`), each wrapped by `WrapRedacted`.
Their string results are returned verbatim to the agent loop. `web_fetch` caps downloads via
`max_bytes` (1 MB default) but injects the whole result inline; there is no context-token budget
guard. Separately, `kg_search.go` and `memory.go` both call `RegisterConfig("Memory", ...)`;
`configRegistry` is a `map[string]ConfigEntry`, so the last `init()` wins (currently `memory.go`, by
filename order) — fragile and order-dependent. The conversation-history compaction layer
(`internal/agent/summarization_scrub.go`) condenses old tool results into stubs classified by tool
name + call args: file-producing tools keep their path; all others reduce to `[cleared <tool>]`.

**Problem:** A single oversized result can exceed the 64k-token context several-fold. The two
resources (RAM vs context tokens) are conflated under one knob (`max_bytes`). The config collision
makes the Memory schema's surface depend on filename ordering. And without an explicit interop, a
spilled result would be condensed to `[cleared <tool>]` on compaction — discarding the spill path and
making the offloaded artifact unrecallable.

**Constraints:**
- Do not duplicate spill logic per tool; one mechanism covers the catalog.
- Spilled artifacts must be consumable by the existing workspace-confined `read_file`.
- Secrets must never reach disk unredacted.
- A spilled artifact must remain recallable by path after compaction.
- Keep FS-middleware tools (self-bounding) and MCP tools out of scope for v1.
- Stay surgical on the config collision fix.

## Goals / Non-Goals

**Goals:** Bound context injection from any factory tool's result; predictable, agent-actionable
return shape (never a silent inline↔file switch); per-group, hot-reloadable threshold decoupled from
`max_bytes`; eliminate the `kg_search`/`memory` config-category collision; keep spilled artifacts
recallable by path across compaction.

**Non-Goals:** Cover FS-middleware or MCP tools in v1; garbage-collect spilled files (keep for
session lifetime); content-type sniffing beyond a default extension.

## Decisions

### Decision 1: Generic decorator, not per-tool logic
One `WrapFileSpill` applied in `Builtin()` next to `WrapRedacted`. Each tool still returns its full
string; the decorator alone decides inline-vs-file. Chosen over per-tool spill code (DRY) and over a
result-block middleware (which would need the call ID and operate on serialized messages — heavier).

### Decision 2: Decorator ordering redacts before spill
`WrapFileSpill(WrapRedacted(invokable))`. Redaction is innermost, so the spilled file and the
envelope preview contain already-redacted content. Secrets never hit disk.

### Decision 3: SessionID on Scope (agent is per-session)
`AssembleAgent` is invoked per session and receives `conversationID`; `sessionID` is already derived
from it (`agent.go:317`) for the hooks `SessionState`. Add `SessionID string` to `Scope`, populate at
the `Scope` literal (`agent.go:155`) with the same derivation. No ctx-key plumbing needed (verified:
one agent instance serves one session).

### Decision 4: Per-call, hot-reload-safe threshold
The decorator holds the tool's `Category()` + `scope.ToolGroupCfg`. At `InvokableRun` it calls
`GetConfig(ctx, category)`, unmarshals only `spill_threshold_bytes` (anonymous struct, mirroring
`memory.go`/`kg_search.go`), and falls back to a per-category default. Reads generically from the
category JSON — no changes to domain config structs (`sysweb.Config`, etc.); only the JSON schemas
gain the field. Decoupled from `max_bytes` (download/RAM cap) — they guard different resources.

### Decision 5: Naming `<tool>_<timestamp>_<title>.md`
Timestamp = sortable UTC `20060102-150405` plus sub-second disambiguation so rapid same-second calls
stay unique. Title = slug of the most meaningful input arg (`url`→host+path for web/browser;
`query`/`seed_entity_name` for searches; fallback content-head slug, then `result`), truncated ~40
chars. Chosen over a content hash (user preference: a human-readable log of what was fetched). Dedup
is relaxed — each call is a distinct timestamped artifact (acceptable under keep-for-lifetime).

### Decision 6: Extension `.md` by default
Spilled content is extracted text that renders as markdown for most tools; default extension `.md`.
A per-tool `ext` hint (e.g. `.json` for JSON-blob tools like `browser_console`) is a trivial future
addition; not built in v1.

### Decision 7: Session-scoped path under the resolved workspace
`spillPath = <Workspace>/.onclaw/workspace/<agent>/sessions/<sid>/tool_results/<file>`. `Workspace`
is the resolved workspace root (`workspace.ResolveWorkspace`), shared and absolute; the `<agent>`
segment namespaces per agent. The envelope returns the workspace-relative tail so `read_file`
(workspace-confined) consumes it directly. Agent/session/tool/title names are slug-sanitized for path
safety.

### Decision 8: Fix the collision by folding kg_search into the Memory schema
`kg_search`'s standalone `RegisterConfig("Memory", kgJSONSchema)` is removed; `max_depth` is added to
the Memory schema owned by `memory.go`. `kg_search` reads `max_depth` from the Memory config (it
already reads other Memory fields) and keeps `Category() == "Memory"`. Chosen over a new
`"KnowledgeGraph"` category (more surface area / UI change) for surgicalness. Result: each tool-group
category has exactly one schema owner, and kg_search's spill threshold resolves from the Memory
config like the other Memory tools.

### Decision 9: Spilled results are durable pointers across compaction
A spilled result's envelope is the live-context representation of an artifact that lives on disk.
The summarizer-input scrub (`internal/agent/summarization_scrub.go`, Layer B of the
conversation-history compaction work) classifies stubs by tool name + call args, so without an
explicit rule a spilled `web_fetch` envelope would collapse to `[cleared web_fetch]` and lose the
spill path. To keep the artifact recallable after compaction, the scrub SHALL also inspect the tool
**result content** for a spilled-artifact path and, when present, promote the stub to the
path-preserving class — `[<tool> -> <spill-path>]` — dropping the envelope preview/body, exactly as
`write_file`/`edit_file` keep their destination path. Detection keys on the envelope's distinctive
path segment `.onclaw/workspace/<agent>/sessions/<sid>/tool_results/`, which the envelope SHALL place
on a dedicated, unambiguous line so it is machine-parseable. The recall-first summarization prompt
already instructs verbatim path preservation and treating stubs as pointers, so no prompt change is
required — only the scrub taxonomy gains a result-content spill-path check.

### Decision 10: Filesystem search uses cap-at-source, not spill
`grep` and `glob` can return large uncapped results (`fsbackend.go` `GrepRaw`/`GlobInfo`), but they
bypass `Builtin()` (injected by the Eino filesystem middleware), so the factory `WrapFileSpill`
decorator does not cover them. Because onclaw owns the backend, the fix is cap-at-source — truncate
in the backend and surface a truncation indicator — matching the existing `execute` 32 KB cap
precedent, rather than retrofitting spill-to-file across the middleware-owned tool surface. Defaults:
`grep` capped at 32 KB of matched content; `glob` capped at 200 entries. Because the Eino middleware
(not onclaw) formats the grep/glob result string, the truncation indicator is surfaced as a synthetic
marker entry appended to the returned slice — the only channel onclaw controls — so the agent learns
results were truncated and should narrow its query. `read_file` needs no cap (already paginates via
`offset`/`limit`); `write_file`/`edit_file`/`ls` return small payloads. Truncation-with-hint is chosen
over file-reference spill here because a narrowed re-query is the appropriate recovery for search
results, whereas spill suits large singular artifacts (a fetched page).

### Decision 11: Image-producing tools persist artifacts and return a path reference
`browser_screenshot` returns a `data:image/png;base64,...` string — a large blob that, traveling as
text in the tool result, both blows context and is not meaningfully visible to the model as an image.
Rather than spill its base64 to a `.md` (useless) or exclude it, the tool SHALL write the raw PNG
bytes to a session-scoped `.onclaw/.../tool_results/browser_screenshot_<ts>_<title>.png` and return a
path-reference envelope, reusing `spillPath` and `Scope.SessionID`. The title is derived from the
active page (URL host / title), falling back to `page`. This is in-tool binary handling (the tool
holds the raw bytes), not the text `WrapFileSpill` decorator; the resulting small envelope then
passes through that decorator unchanged. The `.png` path sits under `tool_results/`, so the Phase 5
summarizer interop preserves it across compaction automatically (`[browser_screenshot -> <path>]`).
Trade-off: the model receives the path, not inline image bytes, so it cannot visually inspect the
screenshot on that turn (it relies on `browser_snapshot` for page vision); returning a
provider-native image content block referencing the file is a future enhancement once tool results
can carry image blocks beyond a plain string.

## Risks / Trade-offs

**Risk:** Spilled `.onclaw/` files surface in the agent's own `glob`/`grep`.
- **Mitigation:** Follow-up adds a `.onclaw` ignore to the FS grep/glob candidate filters
  (`fsbackend.go`). Out of scope here.

**Risk:** Disk growth on 8 GB storage (keep-for-lifetime).
- **Mitigation:** Accepted per decision; bounded by session; revisit if pressure appears.

**Risk:** Title-derivation heuristics mislabel a file.
- **Mitigation:** Sanitizer + truncation keep filenames safe; the timestamp + tool prefix always
  disambiguate. Title is a convenience, not a correctness dependency.

**Risk:** Folding `max_depth` into Memory couples a KG knob to the Memory schema.
- **Mitigation:** The Memory schema is already a grab-bag of category knobs; one more is consistent.
  A dedicated KG category remains a future option.

**Risk:** If the summarizer scrub fails to recognize a spill envelope, compaction reduces it to
`[cleared <tool>]` and the path is lost.
- **Mitigation:** Detection keys on the stable `tool_results/` path segment, not on prose; a test
  asserts the path survives compaction and the preview is dropped.

## Open Questions
- Per-tool extension hints (`.json` for `browser_console`/`browser_tabs`) — defer.
- Whether to add the `.onclaw` ignore to grep/glob in this change or a follow-up.