## 1. Runner: compact-command execution (backend core)

- [ ] 1.1 Add `Command` field to `agents.ExecRequest` (constants `CommandCompact = "compact"`, normalize empty → normal turn) and thread it through the run submission path from the /v1 handler
- [ ] 1.2 In the runner, branch compact-command turns before the normal model turn: load the session's current message window; when there is no compactable history, emit `turn_completed` only (quiet no-op, no compaction event)
- [ ] 1.3 Build the standalone summarization instance per compact turn (`einosumm.NewTyped`: turn ChatModel, same transcript-offload callback as `Compose`, `UserInstruction` = focus text) and run `Summarize` on the loaded state
- [ ] 1.4 Persist the window replacement: append the `SessionEventMessagesReplaced` record to the session store from the runner (verify the adapter accepts runner-appended replacement records; if it requires an event-forwarding context, use the runner-scoped fallback per design D4)
- [ ] 1.5 Emit the compact turn's transcript events: `context_compacted` with `CompactionPayload{TokensBefore, TokensAfter}` (estimator over the Callback's before/after states) then `turn_completed` carrying the summarizer call's usage; fire `run_started`/`run_finished` hooks, skip `user_prompt_submit` (design: hooks interplay)
- [ ] 1.6 Unit tests: compact rewrites window + events order; focus text reaches the instruction; below-threshold compaction executes; empty-history no-op; usage on turn_completed

## 2. Wire: /v1 routing and event delivery

- [ ] 2.1 In the /v1 handler, route `metadata.onclaw_command: "compact"`: resolve session binding (never birth a session — unknown/foreign fails not-found), pass focus from `input`, set `ExecRequest.Command`; ordinary turns unchanged
- [ ] 2.2 Translate `context_compacted` transcript events to a new `onclaw:context_compacted` SSE frame `{type, tokens_before, tokens_after, sequence_number}` on the live stream and the catch-up attach replay; non-streaming compact requests aggregate to `response.completed` with summarizer usage and no output items
- [ ] 2.3 Fill `tokens_before`/`tokens_after` into the History replay projection of `context_compacted` (payload hydration for transcripts read via the events endpoint)
- [ ] 2.4 Tests (handlers/v1): compact stream shape (compacted event → completed with usage → [DONE]); focus not persisted as user message; no-birth not-found; empty-history quiet completion; active-run conflict surfaces as for ordinary turns; aggregate non-streaming response

## 3. Web: command surface

- [ ] 3.1 Gut `COMMANDS` to the single `/compact` entry in `constants.ts`; `SlashMenu` renders it with its description (no other entries anywhere)
- [ ] 3.2 Intercept submission in the agent-chat send path: exact `/compact[ focus]` match submits a compact turn with `metadata.onclaw_command: "compact"` and focus as `input`, never appending a user message; unknown `/foo` sends as ordinary text; channel/team composers never open the command menu and pass `/compact` through as text
- [ ] 3.3 Unit tests: menu lists only `/compact`; interception builds the right request; unknown command passes through; channels unaffected

## 4. Web: status row, divider, wire handling

- [ ] 4.1 Handle `onclaw:context_compacted` in `openresponses.ts`/`livechat.ts`: carry token estimates through to the thread store
- [ ] 4.2 Show the "Compacting context…" status row while a compact turn runs (ThinkingRow-style pending state, no user pill, no optimistic agent row); retract on terminal state — divider on success, nothing on quiet completion or failure
- [ ] 4.3 Add the compaction divider component (`Context compacted · N → M tokens`, muted hairline style per design mockups) rendered for live events and for hydrated `context_compacted` history entries; no agent ack message fabricated
- [ ] 4.4 Unit tests: status row lifecycle (shows → swaps to divider / retracts); divider renders from live event and from hydrated history; token-formatting ("154k → 9.2k")

## 5. Verification

- [ ] 5.1 `go build ./...`, `go vet ./...`, `go test ./...` green; touched web suites + `pnpm build` green
- [ ] 5.2 `scripts/smoke.sh` passes (add a compact-command smoke step: bind a session, run two turns, compact, assert the compacted event and completed-with-usage response)
- [ ] 5.3 Manual browser pass: `/compact` in an agent chat (status row → divider), reload shows the divider, auto-compaction divider, channel composer ignores `/compact`
