# Tasks

## 1. Runtime deltas (Go)

- [x] 1.1 Add `DurationMs int` (`json:"duration_ms,omitempty"`) to `sessions.ToolActivity`; record the running announcement time per call id in the runner and stamp the terminal record in `recordToolResult`; verify a runner test asserts a completed and a failed record both carry a positive duration and a replayed pre-change record carries none
- [x] 1.2 Add `LastInput`/`LastOutput` to `agents.ContextUsage`, filled from the last completed turn's usage block beside `Used`; verify a manager test asserts both values after a completed turn and zero before any turn
- [x] 1.3 Add a `BrowserScreenshot(name)` bound read on the chat service returning base64 PNG bytes with width/height, resolving only `Join(browserDir, "screenshots", name)` after validating `name` against `^\d+\.png$` and `filepath.Base(name) == name`; verify tests cover a valid name, a traversal attempt, a non-matching name, and a missing file (refused / not-found without touching other paths)
- [x] 1.4 Run `gofmt -w .`, `go vet ./...`, `go test ./...`, then `task build` and confirm the regenerated bindings expose `duration_ms`, `LastInput`/`LastOutput`, and `BrowserScreenshot`; verify `npm run build` in `frontend/` type-checks against the new bindings

## 2. Transcript follow controller

- [x] 2.1 Implement `useFollowBottom` (consent flag flipped only by wheel-up, touch drag, and scroll keys; 36px at-bottom threshold on scroll; rAF-coalesced pin while a turn is in flight; `ResizeObserver` on the inner column) and mount it in `Transcript`; verify a jsdom test streams growth into a bottomed viewport and asserts `scrollTop` tracks `scrollHeight`
- [x] 2.2 Make the follow effect yield while a prepend anchor restore is pending and keep the existing height-delta anchor; verify a test loads earlier history during a streaming turn with a scrolled-up reader and asserts the anchored entry does not move
- [x] 2.3 Add the jump-to-latest control above the composer (hidden while following, smooth scroll, re-arms following) and force-follow on the reader's own send and on session switch; verify tests cover: scroll-up shows the control, activating it hides it and pins, sending while scrolled up pins
- [x] 2.4 Add disclosure-pause: a one-frame pause when any `aria-expanded` control or `<summary>` inside the transcript toggles; verify a test expands a card above the fold mid-stream and asserts the viewport offset is unchanged
- [x] 2.5 Verify reduced-motion: smooth scrolling degrades to instant under `prefers-reduced-motion`, with a test toggling the media query stub

## 3. Tool card chrome and registry

- [x] 3.1 Scaffold `features/chat/tools/` (`registry.ts`, `parse.ts`, `toolDisplay.ts`, `renderers/`, `primitives/`) with the body props contract and `resolveBody(name)` returning `FallbackBody` for unknown names; verify unit tests for `parseArgs` (valid, invalid → null) and `parseJSONResult`
- [x] 3.2 Port the sentence grammar into `toolDisplay.ts` for all fifteen builtin tools (intent/outcome forms, chip/literal markup, `execute` command-as-sentence, browser by `args.action`) with no sentence for unknown names; verify table-driven tests cover every tool in both forms plus an MCP name yielding `null`
- [x] 3.3 Load the settings tools catalog (display name, icon key) once into the chat store keyed by name with raw-name fallback; verify a store test resolves a known and an unknown name
- [x] 3.4 Rebuild `ToolCallCard` as chrome: header (icon, display name, sentence, latency, chevron), shimmer label while running with a live elapsed counter, failure styling, in-place update by call id, clamp with show-all, raw toggle, and an error boundary degrading to `FallbackBody`; verify component tests for collapsed/expanded/raw/running→completed/failed/unknown-tool/no-duration states in both themes
- [x] 3.5 Add the bounded collapse-state map (LRU ~800 by call id) and auto-open for `edit_file`/`write_file` on the running→completed edge with explicit-close precedence; verify tests: toggle survives re-render, edit auto-opens once, closed card stays closed on re-report
- [x] 3.6 Replace the `prefers-reduced-motion` shimmer with static text and document the module layout and body contract in a `tools/README.md`; verify the reduced-motion test and that `vp check src` passes

## 4. Interactive tool bodies

- [x] 4.1 Add the `diff` dependency and build `primitives/DiffView` (unified rows, gutter line numbers, `--success`/`--danger` color-mix tints, three-line collapsed context with expand seams, 400-row cap with show-all, memoized by call id); verify tests with a multi-hunk fixture in both themes
- [x] 4.2 Build `primitives/CodeBlock` (language label, copy, wrap toggle, optional line numbers, plain-while-streaming) and `primitives/FileChip` (opens the right-panel preview with the existing jailed path handling); verify tests for copy, wrap, and chip navigation with a refused foreign path
- [x] 4.3 Implement `EditFileBody` (diff from `old_string`/`new_string`, `replace_all` badge, file chip) and `WriteFileBody` (code preview + line count); verify fixture tests from real eino argument shapes
- [x] 4.4 Implement `ReadFileBody` (numbered preview of the lines read) and `ExecuteBody` (terminal-styled command block, output, failure styling); verify fixture tests including a failed command
- [x] 4.5 Implement `GrepBody` (matches grouped by file, pattern highlighted, rows open the preview) and `FileListBody` for `glob`/`ls`; verify tests parse the middleware's content and files-with-matches output modes
- [x] 4.6 Implement `SearchBody` (result cards: linked title, hostname, snippet) and `WebFetchBody` (address chip + clamped text); verify tests with the `{"results":[...]}` payload and a plain-text fetch
- [x] 4.7 Implement `BrowserBody` for navigate/action (address, title, ok badge) and snapshot (ref/role/name table); verify tests with the typed JSON results
- [x] 4.8 Implement the screenshot thumbnail (bound read → Blob object URL, sized from reported width/height, unavailable state on failure) and `primitives/Lightbox` (overlay, Escape and backdrop close, focus return); verify tests for sizing, unavailable state, and lightbox open/close/focus
- [x] 4.9 Implement `TodoBody` (checklist snapshot with pending/in-progress/done states) and `KeyValueBody` for `http_get`/`current_time`, and register every body in `registry.ts`; verify a registry test asserts each builtin name resolves to its body and an MCP name to the fallback
- [x] 4.10 Verify `FallbackBody` renders pretty-printed JSON or verbatim text and that a body throwing on a malformed payload degrades to it without affecting sibling cards (error-boundary test)

## 5. Heavy-turn folding

- [x] 5.1 Add the Transcript grouping pass: consecutive `tool`/`task` entries sharing a turn id form a run; runs of four or more render behind a summary row ("N steps · M files changed", files = distinct completed edit/write paths) with nested cards on expand; verify tests for a five-call run with two edits, and a three-call run that does not fold
- [x] 5.2 Show the shimmering activity label naming the running tool while the turn streams; split runs at pending approvals; tint and reveal on a failed call; collapse repeated `todo_write` calls to "plan updated" except the last; verify tests for each rule

## 6. Context breakdown popover

- [x] 6.1 Port `contextBreakdown.ts` (4 chars/token, 8 per message, 120 per tool, 40 per skill header, 1100 per image; post-compaction entries only; server remainder floored at zero; never scaled; estimated flags); verify unit tests for each rule including the pre-compaction exclusion and the floor
- [x] 6.2 Cache the agent's prompt documents per agent for the Instructions segment and read `LastInput`/`LastOutput` from the store's usage; verify a store test caches one fetch per agent
- [x] 6.3 Build the popover on `ContextMeter` (opens upward, used/limit, 400 ms count-up honoring reduced motion, stacked bar, legend with "estimated" captions, headroom, last-turn rows; no popover without usage); verify component tests for open/close, captions, no-usage, reduced motion, and both themes

## 7. Sessions rail row grammar

- [x] 7.1 Switch rows to compact 26px title-only `SideRow`s (drop the relative-time meta line) with the inline spinner before a pulsing title for running sessions, both static under reduced motion; verify updated rail tests
- [x] 7.2 Cap the unfiltered list to the four most recent plus the active session, with "Show N older sessions" / "Show recent only" controls and a scrolling max height past eight; a non-empty search lists every match; verify tests for the cap, the active-older case, the expander, and search lifting the cap

## 8. Copy-message action

- [x] 8.1 Add the hover/focus-revealed copy button to the assistant action row copying the entry's source text with a 1.2 s check state; fall back to the Wails clipboard binding and raise a danger toast on refusal; verify tests for success, fallback, and refusal

## 9. Streaming markdown and highlighting

- [x] 9.1 Spike: evaluate Streamdown against mid-stream fence handling, compatibility with the right-panel link override, and bundle cost; record the verdict (A adopt / B keep react-markdown) under design.md Open Questions; verify the note names the measured bundle delta and the override outcome
- [x] 9.2 Implement the verdict — (A) adopt Streamdown with the link override preserved and its code rendering routed through `CodeBlock`, or (B) add a dangling-fence pre-render pass to react-markdown and lazy syntax highlighting into `CodeBlock` — and route markdown `pre` through the shared `CodeBlock`; verify tests: an unclosed fence mid-stream renders no runaway block, a completed message highlights with its language label, streaming code stays plain, and the final render equals a one-pass render

## 10. Integration

- [x] 10.1 Run `vp fmt src` and `vp check src`, `gofmt -w .`, `go vet ./...`, `go test ./...`, and finish with `task build`; verify all gates pass and bindings are fresh
- [x] 10.2 Run `openspec validate enhance-chat-ux --strict`; verify the change is valid
- [ ] 10.3 Manual GUI smoke in both themes: stream a long reply (follow, scroll-up pause, jump-to-latest), run an edit that auto-opens its diff, take a browser screenshot and open the lightbox, open the context popover, and expand the sessions rail past four; verify each behaves as its scenario states and note any deviation in design.md
