## Context

The eino summarization middleware (`eino/adk/middlewares/summarization`, v0.10.0-alpha.28) already provides everything a manual trigger needs — verified against module source:

- `TypedMiddleware.Summarize(ctx, state)` is **exported** and self-contained: summarization model call → finalizer (transcript offload, summary message with preamble + continue-instruction) → `Callback(before, after)`. The automatic path is only a token-threshold check (`shouldSummarize`) in front of it.
- `TypedConfig.UserInstruction` replaces the summarizer's user-level instruction — the `/compact <focus>` mechanism for free.
- Our composition (internal/agents/agent.go:191) already wires `Callback` to offload the full pre-compaction transcript into the agent dir.
- The replacement persists as an `adk.SessionEventMessagesReplaced` session event; the runner already mints `context_compacted` transcript events from it (internal/agents/runner.go:1414) and History replay already projects it (internal/agents/history.go:247). The event is simply never forwarded on /v1 and never rendered.

The composer's slash menu is cosmetic: `COMMANDS` (web/src/lib/constants.ts) is the static prototype list; picks insert text that is sent to the model verbatim.

## Goals / Non-Goals

Goals: one real command (`/compact [focus]`), executed as a turn, delivered on the wire, rendered live and hydrated — for manual and automatic compactions alike.

Non-Goals: directives/per-turn hints (`/think`, model pins); skills in the `/` menu (skills stay `$`-only); mid-run steering/queueing; `/new`/`/reset` session lifecycle commands; a user pill or persisted echo of the command; an agent ack message after compaction; commands in channels.

## Decisions

**D1 — Compact-as-a-turn, not a side-channel endpoint.** The compact request enters the normal run pipeline. The active-run guard (409 conflict) serializes compaction against running turns for free; auth, session binding, SSE streaming, catch-up attach, and event persistence are all reused. A dedicated endpoint outside the pipeline was rejected: it would race active runs, need its own write path, and have no stream to attach to.

**D2 — Wire shape: `metadata.onclaw_command: "compact"`, focus in `input`.** The /v1 handler routes on metadata (where session binding already lives) and sets a new `Command` field on `agents.ExecRequest`; `input` becomes the focus text. A compact request binds like any metadata-bound request but MUST NOT birth a session: unknown/foreign `onclaw_session` fails not-found, mirroring `previous_response_id`'s bind-only rule. `previous_response_id`-bound compact requests are allowed.

**D3 — Standalone summarization instance per compact turn.** The runner builds its own `einosumm.NewTyped` instance with the turn's ChatModel, the same transcript-offload callback, and `UserInstruction` = focus text, then calls `Summarize(ctx, state)` with the session's current messages. Reusing the composed agent's middleware instance was rejected: it would force plumbing the instance out of `Compose`'s contract, and the middleware is stateless per call — a second instance with identical settings is equivalent and keeps `Compose` untouched.

**D4 — Persistence and emission by the runner.** The middleware's own `MessagesReplaced` emission requires a run-execution context; the compact turn appends the `SessionEventMessagesReplaced` record to the session store itself (the adapter's append path and its idempotency-by-event-ID guarantees already exist), then emits on the transcript stream: `context_compacted` with `CompactionPayload{TokensBefore, TokensAfter}` filled, then `turn_completed` carrying the summarizer call's usage. Implementation task 2.2 verifies the adapter accepts a runner-appended replacement record; if the adapter only accepts appends from `TypedSendEvent`, the fallback is a runner-scoped event-forwarding context.

**D5 — Token estimates are display-only.** `TokensBefore`/`TokensAfter` come from the `Callback`'s before/after message states using the middleware's default estimator (~4 chars/token). Good enough for `154k → 9.2k` UI copy; never used for billing or trigger math.

**D6 — One wire event, three delivery paths.** New SSE event `onclaw:context_compacted` `{tokens_before, tokens_after}` (matching the `onclaw:` custom-event convention; additive — clients ignoring unknown event types stay valid). Delivered by: the live stream, catch-up attach replay of an in-flight run, and hydration (History replay fills the same payload into the projected `context_compacted`).

**D7 — Client mechanics.** `COMMANDS` becomes `[{ cmd: '/compact', desc: "Compact this conversation's context" }]`. `ChatView`'s submit path intercepts an exact `/compact[ focus]` match in agent chats only and submits a compact turn with `metadata.onclaw_command`; the command text never enters the message list. While running, the thread shows a "Compacting context…" status row in the ThinkingRow style (retracted on terminal state — success swaps in the divider, quiet completion or failure leaves nothing). The divider is a new small component: centered `Context compacted · N → M tokens`, muted tokens/mono, hairline rules, rendered by the same code path for live events and hydrated history. `livechat.ts`/`openresponses.ts` gain the `onclaw:context_compacted` case; hooks/events unchanged.

**D8 — Unknown commands pass through; channels never see the menu.** Any other `/foo` sends as ordinary text in every surface (OpenClaw web behavior — safer than Hermes's prefix-ambiguity resolution). Channel/team composers don't open the command menu at all; forcing "whose context?" there is a later design question.

**Hooks interplay.** A compact run participates in the existing hook lifecycle as a run: `run_started` and `run_finished` (status per outcome) fire; `user_prompt_submit` does not (there is no user prompt — the turn carries no prompt to gate); `pre_tool_use`/`post_tool_use` cannot fire (no tools run).

### UI contract (approved mockups)

**A. Slash menu — `/compact` only:**

```
│  ┌─ Slash commands ───────────────────────────────────┐  │
│  │ /compact   Compact this conversation's context     │  │
│  └────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐
│  │ /compact ▌                                           │
│  │                                            ┌──────┐  │
│  │ 📎                                          │  ↑  │  │
│  └──────────────────────────────────────────────────────┘
```

**B. While compacting (no user pill, no optimistic agent row):**

```
│      ◌ Compacting context…                               │
```

**C. After compaction (manual and automatic render identically):**

```
│  ───────────────  Context compacted ───────────────      │
│     154k → 9.2k tokens · summary saved to transcript     │
```

**D. After reload (hydrated from the session log):**

```
│  ───────────────  Context compacted ───────────────      │
│     154k → 9.2k tokens                                   │
```

## Risks / Trade-offs

- [Runner-appended `MessagesReplaced` record hits an adapter constraint] → D4's fallback (runner-scoped event-forwarding context) is already proven by the middleware's own emission path; task 2.2 resolves which shape lands.
- [Estimator token counts drift from provider counts] → display-only by D5; the context meter's server-published numbers remain the source of truth for thresholds.
- [Compaction lands while a second browser is catch-up-attaching] → the replacement rides the same ordered event stream as every other event; replay converges by construction (proven by automatic compactions today).
- [`/compact` on a huge session takes long with no text feedback] → the status row exists precisely for this; the summarizer call is a single model call, not a loop.
- [Focus text could be prompt-injected into the summarizer instruction] → the summarizer's output replaces conversation history only; it runs with the agent's own model and no tools, so the blast radius equals the automatic path's.

## Migration Plan

Additive only: no migrations, no breaking wire change. Deploy backend and web together for the feature to be reachable (the command needs both); shipping either side alone degrades to today's behavior (unknown `/compact` text, or an event the web ignores). Rollback is a plain revert.

## Open Questions

None blocking. Divider visual polish (exact tokens) follows the design contract at implementation time; the structure above is the binding UI contract.
