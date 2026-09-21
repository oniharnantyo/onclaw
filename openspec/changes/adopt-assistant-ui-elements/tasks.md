# Tasks: adopt-assistant-ui-elements

## 1. Generative-UI registry (frontend, first)

- [x] 1.1 Create the `$type` spec-renderer registry (`web/src/lib/generativeUi/`): dispatch on recognized `$type`, props/`children` pass-through, unknown type → nothing user-visible, renderer registration API
- [x] 1.2 Build the shared stagger-reveal primitive (client-side reveal after arrival; hydrated renders skip the stagger)
- [x] 1.3 Mount the registry in the transcript's tool-card path (ToolCall result/envelope inspection → registry → element, else existing generic rendering)
- [x] 1.4 Coverage-guard test pinning registry entries (mirrors the toolDisplay catalog guard)

## 2. Context measurement (backend)

- [x] 2.1 Instruction composer reports per-section byte counts (persona, workspace/user docs, memory sections, channel docs, profiles) alongside the composed instruction
- [x] 2.2 Measure the conversation segment with `estimateWindowTokens` over the true session window at turn end; derive `files` from in-window attachments
- [x] 2.3 Add `ContextBreakdown` to `UsagePayload` (internal/agents/events.go) with omitempty segments; populate in the usage accumulator path only when provider usage exists
- [x] 2.4 Pass `context_breakdown` through the openresponses translation (translate.go) and gateway usage emission
- [x] 2.5 Unit tests: segment math, omitempty behavior, display-only guarantee (no trigger/billing reads), true-window measurement after compaction

## 3. Context ring + breakdown (frontend)

- [x] 3.1 Rewrite `web/src/lib/contextBreakdown.ts` to honest-remainder semantics (face-value segments, post-compaction conversation counting, floored remainder, headroom) with tests; replaces the parked scale-to-fit version
- [x] 3.2 Extend `recordThreadUsage` consumers to store last-turn input/output (store + runtime.tsx pass-through + livechat hydration of `ev.usage.input_tokens/output_tokens`)
- [x] 3.3 Build the context ring trigger (SVG donut, 65/85 severity ladder, no tick) into the composer's left rail; remove the header meter and its props from ChatHeader
- [x] 3.4 Build the upward popover: used/total with number ticker, segmented bar, legend rows, Server context row with provenance tooltip, Headroom row, turn input/output rows, estimated caption; prefer wire breakdown when present
- [x] 3.5 Migrate ChatHeader meter tests to the composer side (severity tiers, one-decimal rule, hidden cases, breakdown rows, reload restore)

## 4. Web search card (frontend)

- [x] 4.1 Registry entry keyed on parsed `web.search` results: query pill, searching state, "Read N sources" status line, domain-avatar source rows with monospace domains
- [x] 4.2 Stagger reveal on arrival; hydrated history renders fully revealed; unparsable results fall back to the generic card
- [x] 4.3 Tests: card replaces generic rendering, fallback path, stagger/hydration behavior

## 5. Todo list (backend)

- [x] 5.1 Migration `000061_agent_todos` (columns per design D5; UNIQUE (session_id, item_key); INDEX (workspace_id, agent_id, status); workspace/agent FKs CASCADE) + down
- [x] 5.2 `storeport` todo store + fake: Get by session, Replace (transactional upsert-by-key/delete-vanished/revision bump), workspace-scoped reads
- [x] 5.3 `todo_write` tool: schema validation (4 statuses, required key/text), transactional replace, echo result; rejection leaves stored state untouched
- [x] 5.4 `todo_read` tool returning the session's current list + revision
- [x] 5.5 Register both tools in the catalog ("Todos" group, toggleable) and the runner toolset; wire agent exposure
- [x] 5.6 Open-items context injection: one-line summary (counts by status) composed per turn for agents with the tool exposed
- [x] 5.7 Tests: fake-store unit tests, transactional replace semantics, validation-no-mutation, tool-loop round trip; postgres integration test for the store

## 6. Todo checklist card (frontend)

- [x] 6.1 Registry entry rendering `todo_write` args: `n/m · rev` header (done-only numerator, failed-in-denominator), four row states, failed reason line, restyle-in-place by item key
- [x] 6.2 Collapse earlier same-turn rewrites to one-line "plan updated" summaries; newest card expanded
- [x] 6.3 Tests: states/ratio math, rewrite collapse, present-only behavior

## 7. Echo tools + structured cards (backend + frontend)

- [x] 7.1 `ui.chart`, `ui.timeline`, `ui.preview` Go tools: schema validation + echo, catalog entries (toggleable), runner registration
- [x] 7.2 Chart card: label/value/delta header (sign-tinted), area/line/bars sparkline with visible-count clamp and newest-point emphasis; malformed series renders header only
- [x] 7.3 Web preview card: URL bar (host), reload (remount by key), open-in-new; sandboxed iframe without `allow-same-origin`; null while running; failure without blank frame
- [x] 7.4 Timeline card: vertical axis, envelope ordering, settled vs. reference event styling
- [x] 7.5 Tests per card (envelope → render, malformed → graceful, sandbox attributes asserted)

## 8. Message queue (frontend)

- [x] 8.1 Queue state in the store (ordered items per chat; enqueue on send-while-running; remove cancels only that item)
- [x] 8.2 Queue stack component between transcript and composer: running row, ordered cancelable rows, present-only chrome
- [x] 8.3 Auto-dispatch first queued item through the normal send path on run completion (409 falls back to the existing catch-up flow)
- [x] 8.4 Tests: enqueue/remove/dispatch ordering, cross-tab conflict path unchanged

## 9. Transcript polish (frontend)

- [x] 9.1 Message timing: capture first-token/total/tok-s on live turns (parked runtime wiring kept), hover line near actions, present-only; hydrated shows nothing
- [x] 9.2 Day separators: date-boundary dividers (Today/Yesterday/date) + hover full timestamps; ISO `at` on live entries alongside display `ts`
- [x] 9.3 Draft restore: per-thread localStorage draft, restore on return, clear on send, never synced
- [x] 9.4 Tests: divider boundaries with mixed dated/undated entries, timing present-only, draft lifecycle

## 10. Tool group timeline collapse (frontend)

- [x] 10.1 Collapse header for turns with ≥4 tool calls: "N steps · M files changed" with churn derived from edit_file old/new_string
- [x] 10.2 Expand reveals the inline cards in stream order; <4-call turns unchanged; user-controlled open state
- [x] 10.3 Tests: threshold behavior, churn derivation, light-turn passthrough

## 11. Math rendering (frontend)

- [x] 11.1 Add remark-math + rehype-katex; wire into MarkdownBody with lazy KaTeX CSS loading when math delimiters are present
- [x] 11.2 Tests: inline/block math render, non-math messages unaffected

## 12. Verification

- [x] 12.1 `go build ./... && go vet ./... && go test ./...` green; `go test -tags=integration ./...` for the todo store
- [x] 12.2 `pnpm test` green including new suites; `pnpm build && pnpm lint` clean
- [x] 12.3 toolDisplay coverage-guard updated for the new catalog entries; registry guard test green
- [x] 12.4 `openspec validate adopt-assistant-ui-elements --strict` clean
- [x] 12.5 Live pass: agent chat turn exercises ring → popover breakdown (server numbers present), a todo_write turn renders the checklist card and re-grounds after /compact, a ui.chart and ui.preview turn render their cards, queue/draft/timing/separators behave in one session
