# Design

## Context

See proposal.md — Why. The relevant current state:

- **Transcript** (`frontend/src/features/chat/components/Transcript.tsx`): a plain `overflow-y-auto` div; one layout effect compensates prepends from "load earlier" and nothing pins the tail. Streaming mutates the last entry in place, so no length-keyed effect could catch deltas. Tool calls, reasoning, approvals, and tasks are **separate transcript entries** (`Entry.kind`), not parts inside the assistant message — onclaw-v2 keeps `tools[]` inside the message, so its folding and interleaving logic does not transplant directly.
- **Tool payload** (`internal/sessions/types.go` `ToolActivity`): `call_id, name, arguments (string), status, result (string), is_error`. No duration. The runner announces the running form with **complete arguments** after the model call finishes (`runner.go` `calls.finish()`), and persists only terminal records (design D10 of integrate-agent-chat): the running state is live-only.
- **Tool argument/result shapes** are fixed by their implementations: eino's filesystem middleware (`file_path`, `old_string`, `new_string`, `replace_all`, `content`, `offset`, `limit`, `pattern`, `path`, `glob`, `output_mode`, `command`), the browser tool's typed JSON results (`{url,title,elements[]}`, `{path,width,height}`), web search's `{"results":[{title,url,snippet}]}`, todo's `{items[{key,text,status,reason}],revision}`. Results above ~20k tokens are offloaded by the middleware to a `/large_tool_result/<call_id>` stub path. Tool display names and icon keys exist in the Go catalog and reach the frontend through the settings tools listing.
- **Context usage** (`internal/agents/context.go`): `used = FinalInputTokens + OutputTokens` of the last completed turn; the manager exposes `{used, limit}` and the store pulls it after each terminal event. Agent persona text is reachable through the prompt-documents read.
- **Attachments** reach the webview by a bound read keyed by an unguessable capability key that returns base64; the renderer builds a Blob object URL. Raw paths never cross the boundary.
- **Sessions rail** (`SessionRail.tsx`) is the island's left column with search, drag-resize, confirm-to-delete, and kit `SideRow`s carrying a relative-time meta line and an end-of-row running dot.
- **Project constraints**: both themes for every surface; the design contract is the token file plus the `components/ui` kit; `@assistant-ui/react` and `@assistant-ui/react-markdown` are declared dependencies with no imports anywhere in `frontend/src`.

## Goals / Non-Goals

**Goals:**

- One scroll controller owns following, pausing, resuming, and the prepend anchor, so the four scroll behaviors never fight each other.
- Tool rendering is a registry: adding a renderer for a new tool is one file and one map entry; the fallback is always correct for unknown tools.
- Every interactive body is derivable from the persisted record (args + result), so reloaded transcripts render identically to live ones.
- Nothing in the card layer invents information: no sentence for unknown tools, no fabricated duration, no scaled estimates in the context breakdown.

**Non-Goals:**

- Virtualizing the transcript (windowing already bounds it).
- Server-side result envelopes or a tool-output schema registry in Go.
- Binding the transcript to the assistant-ui runtime; adopting or removing its packages.
- Live partial-argument streaming (the runner commits whole calls).

## Decisions

### D1. Autoscroll: a hand-rolled follow controller, not a library

Port onclaw-v2's three effects and ZCode's consent semantics into a `useFollowBottom` hook owned by `Transcript`:

- `following` is a **consent flag**, not a geometry snapshot. It flips to `false` only on genuine user input that moves the viewport up — `wheel` with negative deltaY, touch drag, and scroll keys (PageUp, ArrowUp, Home, Shift+Space) — and back to `true` when a scroll event lands within **36px** of the bottom (onclaw's threshold). Programmatic scrolls never change it, which fixes the race where a streaming delta commits between an up-scroll and its scroll event.
- A **follow effect runs after every render while a turn is in flight** (`busy` or any entry streaming) and sets `scrollTop = scrollHeight` **synchronously** when `following` — never deferred to an animation frame, because `requestAnimationFrame` is suspended whenever the host stops painting (occluded or minimized window, offscreen webview) and a deferred pin leaves a stream that ends in that state short of the tail (found in the task 10.3 smoke: a webview with frames suspended sat 1057px above the tail with following still armed). A `ResizeObserver` on the inner column catches late growth (image attachments decoding, code blocks reflowing) and re-pins through a rAF-coalesced path, where coalescing bursts of layout changes is what matters.
- **Own sends force-follow**: dispatching a message sets `following = true` and jumps, so the reply starts in view even if the reader had scrolled up.
- **Session switch** resets to the bottom; **prepend** keeps the existing height-delta anchor and, while an anchor restore is pending, the follow effect yields (onclaw's `anchorPendingRef` rule) so the two never contend for one commit.
- **Disclosure-pause**: toggling any `aria-expanded` control or `<summary>` inside the transcript sets a one-frame pause so the content height change from opening a card does not yank the viewport; `following` is left as it was.
- **Jump-to-latest**: a floating circular control above the composer, `invisible` while `following`, scrolls smoothly to the bottom and re-arms following on arrival.

Alternatives: vendoring assistant-ui's `createThreadViewportAutoScroll` (~460 lines, MutationObserver-based, content-inset registry — more than we need, and importing it from `@assistant-ui/store/client` ties us to a weekly-churning 0.x subpath); `use-stick-to-bottom` (adds a dependency and its smooth-scroll animation model for ~60 lines of logic we can own). The hand-rolled controller is what onclaw-v2 ships in this exact design language.

### D2. Tool cards: card chrome + a renderer registry

`ToolCallCard` becomes chrome only — header row, status, latency, chevron, fold participation — and delegates its body to `REGISTRY[call.name] ?? FallbackBody` in a new `features/chat/tools/` module:

```
tools/
  registry.ts           name -> body component; FallbackBody; `resolveBody(name)`
  toolDisplay.ts        sentence grammar (D3), latency/size formatting
  parse.ts              parseArgs(argsText) -> object | null; parseJSONResult(result) -> unknown | null
  renderers/            EditFileBody WriteFileBody ReadFileBody ExecuteBody GrepBody
                        FileListBody (glob, ls) SearchBody WebFetchBody BrowserBody
                        TodoBody KeyValueBody (http_get, current_time) FallbackBody
  primitives/           DiffView CodeBlock FileChip Lightbox ClampedText RawToggle
```

Every body receives one props contract: `{ call, args, result, isError, status, durationMs, openPath(path) }` where `args` is the parsed JSON (or `null` when parsing fails — the body then falls back to the raw text pane) and `result` is the raw string with a lazily parsed JSON view. Each body mounts inside an **error boundary that degrades to `FallbackBody`**: a malformed payload can never blank the card. Bodies are pure functions of the persisted record, so live and reloaded transcripts render the same.

Alternatives: onclaw's single `switch` inside `ToolCall.tsx` (not extensible, mixes chrome and bodies); assistant-ui's `MessagePrimitive.Parts components.tools.by_name` (the same registry idea, but it only works inside their runtime context).

### D3. One-liner sentences: onclaw's grammar, buddies' tool table, no inventions

Port `toolDisplay.ts`: a sentence table keyed by tool name with an intent form while running and an outcome form when done, a tiny markup grammar (`` `x` `` renders a mono chip, `'x'` a quoted literal), and the `execute` special case where the command *is* the sentence. The table covers the fifteen builtin tools (`read_file` → "Reading `path`" / "Read `path` · N lines", `edit_file` → "Editing `path`" / "Edited `path`", `web_search` → "Searching 'query'" / "Found N results", browser actions by `args.action`, …). **Unknown and MCP tools mint no sentence**: the header shows the display name and the status word only. Display name and icon come from the settings tools catalog, fetched once into the chat store and keyed by name, with the raw name as fallback.

### D4. Diff: jsdiff line diff rendered as a unified view

`edit_file` bodies compute `diffLines(old_string, new_string)` with **jsdiff** (`diff` on npm — zero dependencies, MIT, ~15 KB) and render a unified view: line numbers in a gutter, `+`/`−` rows tinted with `color-mix` of `--success` and `--danger` so both themes stay legible, unchanged runs collapsed to three lines of context with an "expand" seam, a `replace_all` badge when set, and a cap of 400 rendered rows with "Show all" beyond it. `write_file` renders its content as a plain code preview (there is no "before" to diff against). Intra-line word highlighting (`diffWords` on changed pairs) is a refinement task, not a requirement.

Alternatives: hand-rolled LCS (reinventing a solved problem); `@pierre/diffs` as ZCode uses (heavy, Shadow DOM, needs its own Shiki pipeline); a side-by-side view (rejected in scope — narrow transcript column).

### D5. Duration: stamped by the runner on terminal records

The runner already keeps a per-`CallID` map from the running announcement to the terminal record (for arguments). It grows to hold the announcement time; `recordToolResult` sets `DurationMs = now − startedAt` on the terminal `ToolActivity`. The field is additive in the JSON payload — no migration. Clients show a live elapsed counter for a running card from the moment its running event arrived, then swap to the persisted duration; reloaded transcripts show the persisted value only. A record without the field (persisted before this change) shows no latency rather than a fabricated one. A measured call that finishes inside a millisecond — local file tools routinely do; the task 10.3 smoke persisted every `write_file`/`edit_file` record without `duration_ms` until this was caught — floors to 1 ms so the record still states a latency instead of reading as unmeasured.

### D6. Screenshots: a bound read by validated basename, same shape as attachments

A `BrowserScreenshot(name)` bound call returns `{name, width?, height?, data (base64 PNG)}`. `name` must match the exact shape the tool writes (`^\d+\.png$`); the path is `Join(browserDir, "screenshots", name)` only after `filepath.Base(name) == name` — a traversal attempt is refused before the filesystem is touched, mirroring the attachment store's key-shape rule. The frontend turns the bytes into a Blob object URL (reusing the attachment viewer's helper), renders a thumbnail sized from the result's `width/height` to avoid layout shift, and opens a `Lightbox` (modal overlay, Escape and backdrop close, focus returned to the trigger) on click. In server mode the call rides the gated runtime transport like every other bound call.

Alternatives: base64 in the tool result (bloats the transcript and the model's context); an HTTP asset route (new surface to gate; the bound-read pattern already exists); reading through the workspace jail (screenshots live outside workspaces by design).

### D7. Context breakdown: onclaw's estimator at face value

`contextBreakdown.ts` ports onclaw's rules: four client segments — **Instructions** (the agent's three prompt documents, fetched once per agent and cached), **Tools & skills** (120 tokens per enabled tool, 40 per skill header), **Files** (1100 per image attachment; text attachments by size), **Conversation** (4 chars/token plus 8 per message) — counted over **post-compaction entries only**, and a **Server context** remainder `max(0, used − Σ estimates)` for everything the client cannot see (compaction summaries, tool schemas, retrieved memory). Estimates render with an "estimated" caption and are **never scaled** to the provider's total; the total itself is always the real `used`. Headroom is `limit − used`. Last-turn rows read `LastInput`/`LastOutput`, which the manager adds to `ContextUsage` from the same completed-turn usage block it already inspects.

The popover opens upward from the ring (`bottom-full`, 18rem wide), with a 400 ms eased count-up on open that respects `prefers-reduced-motion`, a stacked 6px bar, legend rows, and both themes via the token palette.

### D8. Heavy-turn fold over entries, not parts

Because tool calls are separate entries, the fold is a **Transcript-level grouping pass**: consecutive `tool` and `task` entries sharing one `turnID` form a run; a run of **four or more** collapses behind a summary row — "N steps · M files changed" (files = distinct completed `edit_file`/`write_file` paths) — with a shimmering activity label naming the last running call while the turn streams. Expanding the row nests the cards under a left hairline rail. **Approval entries never fold**: a pending approval splits the run so it stays actionable, and a resolved one renders inline. Repeated `todo_write` calls in one turn collapse to "plan updated" except the last.

Alternative: folding in the store (presentation state does not belong in the event reducer).

### D9. Collapse state and auto-open

Open/closed state lives in a **module-level bounded map** keyed by `call_id` (LRU, ~800 entries — ZCode's number) rather than React state, so a card re-mounted by windowing, variant switches, or folding keeps the reader's choice. Cards start collapsed. `edit_file` and `write_file` auto-open **once** on the running→completed edge (the diff is the point of the card); `read_file` stays collapsed — its one-liner already says what happened and its body can be long. A user's explicit close always wins over auto-open.

### D10. Running state: shimmer text, not a spinner

The running card's label shimmers (a CSS gradient sweep on the text) and the status word reads "running"; under `prefers-reduced-motion` the shimmer is replaced by static muted text. ZCode's rationale applies: a spinner held in the render tree through high-frequency streaming re-renders is the costliest animation on the page. The sessions rail is the exception — one inline spinner per running session plus a pulsing title is onclaw's row grammar and is bounded by the session count.

### D11. Sessions rail: cap, expander, and search interplay

The unfiltered list shows the four most recent sessions plus the active one if it is older; "Show N older sessions" reveals the rest (the list scrolls within a max height past eight), and "Show recent only" collapses back. A non-empty search lifts the cap — filtering shows every match. Rows become title-only `SideRow`s at 26px; the relative-time meta line is dropped; the running indicator moves inline (spinner before the title, title pulsing).

### D12. Copy action

A hover-revealed button in the assistant message's existing action row (beside Regenerate) copies the entry's **raw markdown source** and shows a check mark for 1.2 s. It uses `navigator.clipboard.writeText` on the click gesture; if the webview refuses, the Wails clipboard binding is the fallback, and a failure surfaces as a danger toast rather than a silent no-op.

### D13. Streaming markdown: a gated spike, library-agnostic spec

The spec requires that streaming never paints transient artifacts from unclosed fences or emphasis and that code blocks highlight once a message completes. A **spike task** evaluates Streamdown against three criteria — mid-stream fence handling, compatibility with the existing component overrides (the link renderer that opens the right panel is load-bearing), and bundle cost — and records a verdict in this document. Outcome A: adopt Streamdown (its Shiki-based code block satisfies the highlighting requirement; `enableSyntaxHighlighting={!streaming}`). Outcome B: keep react-markdown, add a pre-render pass that closes dangling fences while streaming, and wire lazy Shiki into the shared `CodeBlock`. The dependent highlighting task starts only after the verdict.

### D14. One shared `CodeBlock`

Markdown `pre`, `write_file`/`read_file` previews, and `execute` output all render through one `CodeBlock` primitive: a header with the language label, a copy button, and a wrap toggle; content plain while streaming and highlighted lazily afterwards (through whichever path D13 selects). Line numbers are a prop so file previews show them and shell output does not.

### D15. assistant-ui: patterns borrowed, runtime not adopted

> **Superseded by `adopt-assistant-ui-chat`.** The runtime IS adopted now: the chat view runs on the
> assistant-ui external-store runtime over the same store, and the whole transcript, tool-card, run-surface and
> composer layer renders through the vendored element set. The decision below ("neither binds the transcript to
> the runtime nor removes the packages") no longer holds — the packages are bound and the hand-rolled
> equivalents are retired. See `openspec/changes/adopt-assistant-ui-chat/design.md` (D1, D13, D15) for the
> binding decision and the re-vendoring recipe.

The declared `@assistant-ui/*` packages have no imports. onclaw-v2 uses the library only as a thin external-store adapter and renders every transcript element itself; its best artifacts for this change are a scroll algorithm and a content-part type model, both reflected above as hand-owned code. This change neither binds the transcript to the runtime nor removes the packages — the project context names assistant-ui as the chat stack, so that decision is the user's and sits outside this change.

## Risks / Trade-offs

- [Follow effect and prepend anchor contend for one commit] → The anchor-pending gate yields the follow effect during a prepend restore; the two are tested together with a scrolled-up reader loading earlier history while a turn streams.
- [Estimated breakdown misleads] → Every estimated row carries the caption; segments never scale; the remainder is explicitly labeled as server-side context.
- [Huge `old_string`/`new_string` pairs] → Row cap with "Show all"; diff computation runs once per record (memoized by `call_id`).
- [Screenshot read widens the filesystem surface] → Basename-only lookup with a strict name shape; PNG only; one directory; refused before any `os` call; covered by traversal tests like the attachment store's.
- [Shimmer and count-up under reduced motion] → Both degrade to static rendering through one media-query check.
- [Fold hides something actionable] → Approvals split runs; a failed call inside a folded run tints the summary row and the row expands on first failure within the turn.
- [Clipboard refused by the webview] → Binding fallback, then a toast; never a silent failure.
- [Streamdown spike finds the link override incompatible] → Outcome B is fully specified and costs no spec change.
- [Collapse map grows unbounded across sessions] → LRU bound; the map is a cache, not state — losing an entry only resets a card to collapsed.

## Migration Plan

No database migration: `DurationMs`, `LastInput`, and `LastOutput` are additive JSON fields; older records render without latency or last-turn rows. Bindings regenerate after the Go changes (`task build` before frontend gates). Waves land independently (tasks.md); each is revertible on its own since the registry and controller are new modules and the old `ToolCallCard` is deleted only when the registry is wired. Rollback is a frontend revert — no stored data changes shape.

## Open Questions

- Streamdown adopt-or-keep (D13) — **resolved: verdict B** (keep react-markdown, add a dangling-fence pre-render pass, wire lazy Shiki into the shared `CodeBlock`). Spike measured against streamdown@2.7.1 (2026-10-19):
  - (a) Mid-stream: Streamdown runs the `remend` preprocessor (`parseIncompleteMarkdown`, default on) which closes unterminated fences, emphasis, inline code, and links before parsing — the same job as outcome B's pre-render pass, so the streaming-safe spec is meetable either way; there is no correctness win from adopting.
  - (b) Overrides: compatible. The `components` prop fully replaces defaults and `defaultComponents.a` composes, so the load-bearing right-panel link override ports directly. Fenced blocks always emit `pre > code`, so a `pre` override still receives Shiki-highlighted output and can route it through `CodeBlock`; overriding `code`, however, replaces the entire highlighting pipeline (inline, block, mermaid) — `CodeBlock` routing must ride `pre`, not `code`.
  - (c) Bundle (bundlephobia, min/gzip): the current react-markdown@10.1.0 stack is 114/34 KB; streamdown's core is 505/154 KB (it bundles `marked`, `tailwind-merge`, and the rehype-raw/sanitize parse5 chain), so the core swap alone is **+392 KB min / +120 KB gzip (~4.4x)** before highlighting. The `@streamdown/code` Shiki plugin adds ~209/65 KB plus lazy per-language chunks (300–800 KB min each, on demand). Under B, the same Shiki bytes are paid once, lazily, inside `CodeBlock`.
  - Net: B delivers the identical visible behavior for a ~30-line pre-render pass and no new render dependency; A buys the remend logic at a +120 KB gzip core cost we would not otherwise pay. Note for 9.2: D13's `enableSyntaxHighlighting={!streaming}` prop no longer exists in v2 — highlighting is gated by conditionally registering the code plugin, which B's `streaming` prop on `CodeBlock` expresses more cheaply.
- Final estimator constants for the Tools & skills segment once MCP tool counts are visible to the client — a tuning question; the spec only requires face-value estimates with captions.
- Whether intra-line word highlighting (D4) ships in this change or the next — a polish decision inside the `edit_file` body.
