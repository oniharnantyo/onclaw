# Tasks

Phased; each phase is independently verifiable. Phases 0–7 map to `design.md`.

## Phase 0 — SessionID threading
- [x] 0.1 Add `SessionID string` to `Scope` (`internal/agent/tools/tools.go`, after `AgentName`).
- [x] 0.2 Populate `SessionID: strconv.FormatInt(conversationID, 10)` at the `Scope` literal
  (`internal/agent/agent.go:155`), reusing the `agent.go:317` derivation.

## Phase 1 — Spill decorator
- [x] 1.1 Create `internal/agent/tools/spill.go`: `WrapFileSpill`, `fileSpillTool` (embeds
  `tool.InvokableTool`), and `InvokableRun` (run inner; pass error through unchanged; spill only on
  the success path when over threshold).
- [x] 1.2 `resolveSpillThreshold(ctx)` — `scope.ToolGroupCfg.GetConfig(ctx, category)` + anonymous-
  struct unmarshal of `spill_threshold_bytes`, with a per-category default fallback.
- [x] 1.3 `deriveTitle(tool, inputJSON, content)` + slug sanitizer; `spillPath(scope, tool, ts, title)`;
  preview helper (first N bytes); timestamp (`20060102-150405` + sub-second disambiguation).
- [x] 1.4 Envelope builder — stable shape: tool name, result byte size, configured threshold, the
  spill path on a **dedicated line**, truncated preview, and a `read_file` recovery hint.

## Phase 2 — Wiring + config schemas
- [x] 2.1 In `Builtin()` (`internal/agent/tools/registry.go`) wrap each tool as
  `WrapFileSpill(WrapRedacted(invokable), spillCfg{scope, t.Category(), t.Name(), default})`.
- [x] 2.2 Add `spill_threshold_bytes` (integer, default per category) to the JSON schemas: Web
  (`internal/agent/tools/web/register.go`, default 12288), Memory (`internal/agent/tools/memory.go`
  `jsonSchema` + `lastCfg`, default 16384), Browser (`internal/agent/tools/browser/register.go`,
  default 16384).

## Phase 3 — Config collision fix
- [x] 3.1 Add `max_depth` (integer, default 3) to the Memory schema (`memory.go` `jsonSchema` +
  `lastCfg` default).
- [x] 3.2 Remove `kg_search.go`'s `RegisterConfig("Memory", ...)` call and the `kgJSONSchema`
  constant; read `max_depth` from the Memory config inside the `kg_search` handler.

## Phase 4 — Tests + verify (spill core)
- [x] 4.1 `internal/agent/tools/spill_test.go` (black-box `package tools_test`, ≥70% of `spill.go`):
  under-threshold inline (no file); over-threshold spill (envelope + file + preview); title derivation
  per tool family (`url`/`query`/`seed_entity_name`/fallback); timestamp uniqueness for rapid calls;
  error pass-through (no spill on inner error); redaction-before-spill (preview + file masked);
  config override beats default; path sanitization (no `..`/separator escape); preview truncation;
  envelope places the spill path on a dedicated, parseable line.
- [x] 4.2 Regression: `kg_search` `max_depth` still applied when read from the Memory config; the
  Memory category has exactly one schema owner after startup.
- [x] 4.3 Verify: `make vet`, `go test ./internal/agent/tools/... -count=1`, `make build`.

## Phase 5 — Summarization interop (durable spill pointer)
- [x] 5.1 Extend `internal/agent/summarization_scrub.go`: when stubbing a tool result, inspect the
  result content for a path under `.onclaw/workspace/<agent>/sessions/<sid>/tool_results/`; if
  present, emit `[<tool> -> <spill-path>]` (path preserved, preview/body dropped) instead of
  `[cleared <tool>]`.
- [x] 5.2 Black-box test (`internal/agent/summarization_scrub_test.go`): a spilled-result envelope in
  the compacted range yields a path-preserving stub; the spill path survives and the preview does not.
- [x] 5.3 Verify: `openspec validate --changes`.

## Phase 6 — Filesystem search caps (grep/glob)
- [x] 6.1 In `internal/agent/tools/fsbackend.go` `GrepRaw`: cap total matched content (default 32 KB);
  when exceeded, stop appending matches and add a synthetic marker `GrepMatch` indicating truncation
  and prompting a narrower pattern/path.
- [x] 6.2 In `GlobInfo`: cap entry count (default 200); when exceeded, append a synthetic `FileInfo`
  marker indicating truncation and prompting a narrower glob.
- [x] 6.3 Tests (`fsbackend_test.go`): broad grep truncates and surfaces the marker; wide glob
  truncates and surfaces the marker; bounded searches return in full with no marker.

## Phase 7 — Image artifact persistence (browser_screenshot)
- [x] 7.1 In `internal/agent/tools/browser/screenshot.go`: after `page.Screenshot`, write the raw PNG
  bytes to the session tool_results dir via `spillPath` (ext `.png`; title from the active page
  URL/title, fallback `page`), reusing `Scope.SessionID`; return a path-reference envelope instead of
  the base64 data URL. The resulting small envelope passes through `WrapFileSpill` unchanged.
- [x] 7.2 Test (`browser_test.go`): screenshot writes a `.png` under tool_results and returns an
  envelope with a workspace-relative path; no base64 data URL in the result.

## Cross-cutting
- [x] C.1 On archive, sync the `agent-tools` and `conversation-history` deltas into their main specs.
- [x] C.2 Keep MCP tools and the FS-middleware tool surface unchanged; only the onclaw-owned grep/glob
  backends gain result caps (Phase 6). FS tools are not wrapped by `WrapFileSpill`.
- [x] C.3 Preserve the cache-stability invariant: the summarizer interop only changes what is
  condensed for the summary model, never the live replay prefix.
