# Tasks: chat-context-meter

## 1. Backend: final-call input capture

- [x] 1.1 Add `FinalInputTokens` to `agents.UsagePayload`; update `usageOf` all-zeros check to include it
- [x] 1.2 Runner: at each usage accumulation site (streaming frames, agentic message metadata, span events), record the latest call's input into `FinalInputTokens` instead of summing
- [x] 1.3 History rebuild (`internal/agents/history.go`): derive per-turn final-call input from the last span-model-end input alongside the existing totals
- [x] 1.4 Unit tests: multi-call turn reports summed input 135k + final-call 50k; single-call turn reports equal values; reload rebuilds final-call input; all-zero usage still yields nil usage

## 2. Backend: wire and payload exposure

- [x] 2.1 Add `final_input_tokens` to the `/v1` wire `Usage` struct and map it in `UsageFromDomain` / translator paths
- [x] 2.2 Export `agents.DefaultSummarizationMargin`; add `effective_context_window` + `summarization_trigger_tokens` computed response fields in the agents handler using `domain.ResolveContextWindow(a.ContextWindow, nil)`; ensure create/update requests ignore them
- [x] 2.3 `/v1` integration/unit tests: `response.completed`/`incomplete`/`failed` carry `usage.final_input_tokens`; no provider usage omits the block
- [x] 2.4 Handler tests: stored window echoes effective + trigger (50k → 37,500); unset window → 200,000; POST/PATCH with the new fields ignores them

## 3. Web: capture and state

- [x] 3.1 `openresponses.ts`: capture `usage` (incl. `final_input_tokens`) on `response.completed` / `response.incomplete` / `response.failed` and pass to callbacks (stop dropping it)
- [x] 3.2 Runtime/thread state: store latest `{finalInput, at}` per thread on terminal events; thread switch isolates values
- [x] 3.3 `livechat.ts` hydration: read `final_input_tokens` from rebuilt `turn_completed` events so reload restores the meter
- [x] 3.4 Vitest: terminal-event capture (all three event kinds), no-usage → null, hydration restore, per-thread isolation

## 4. Web: header meter UI

- [x] 4.1 Meter component in `ChatHeader` top-right control row (left of member stack/configure): compact bar + mono `%`, hover tooltip `used / window` (e.g. `68k / 200k`), per design tokens
- [x] 4.2 State logic: hidden when target isn't an agent, no usage yet, or usage absent; amber at `>= summarization_trigger_tokens`; neutral below
- [x] 4.3 Component tests: fills per turn (68k/200k → 34% + hover text), warn threshold, hidden in channels/DMs, hidden without usage, reload restore

## 5. Verification

- [x] 5.1 `go build ./... && go vet ./... && go test ./...` green
- [x] 5.2 Web touched suites green (`pnpm vitest run` on modified files) + `pnpm build` passes
- [ ] 5.3 Manual pass: live chat shows meter advancing per turn, warn state reachable on a small-window agent (set `context_window` low), reload restores value, channels show no meter
