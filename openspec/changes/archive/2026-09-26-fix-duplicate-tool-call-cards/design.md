# Design

## Context

A GLM turn on the failing chat made two parallel tool calls (`execute`, `web.search`). The persisted raw event log (`session_events`) shows each call announced twice over separate eino lanes — a `message` event with `function_tool_call` blocks, then `span.tool_call_start` spans cross-referencing the same ids — while the live `/v1` wire delivered only span-sourced starteds (args empty). The client's final `msg.tools` for that turn was `[execute, web.search, web.search]`, and the crash's duplicate id was `web.search`, which pins the wire sequence: `added(execute,"")`, `added(web.search,"")`, `done(call_id=web.search, args="")` (the first finished mislabelled with the last started's id), second `done` swallowed, outputs for both calls. See the proposal for the defect chain and the memory note `duplicate-tool-call-key-explore` for the full evidence.

Two structural facts constrain the design:

- **The span lane is the only live tool-call source on the agentic react path.** The ADK react graph inserts a stream branch after the model node that consumes the stream and reroutes tool-call rounds to the tool path (`adk/react.go:543`); the message-frames lane in `runner.go` can therefore never see the tool-call frames of an executed call. Source code reading and the error-id math agree.
- **Hydration is already correct.** `history.go` translates spans only and joins arguments onto them via `historyJoins.toolArguments` (following the span's `AssistantMessageEventID` back to the persisted assistant message). The fix makes the live path use the same idea; the hydration path is untouched.

## Goals / Non-Goals

Goals:

- Live stream carries exactly one started + one finished per call id, with arguments, for span-sourced (GLM/agenticopenai) calls and any future lane configuration.
- The `/v1` wire pairs items per call id under parallel calls.
- The web bridge is immune to malformed pairings: one card per call id, no duplicate `toolCallId` parts, no tree crash from any future duplicate source.

Non-Goals:

- No change to the wire *format* (existing event kinds and payloads), the DB schema, or hydration behavior.
- No pursuit of the GLM stream-tail latency anomaly (dead window between message completion and model span end) — recorded as a separate observation, out of scope.
- No change to classic-lane providers' emission paths (they don't route through the agentic runner's span lane; their behavior is unchanged by these edits).

## Decisions

### D1: Span lane goes through the guarded helper, with arguments joined from the assistant message

The span branch in the runner's live loop currently emits `tool_call_started` directly with only `CallID`/`Name`. It will instead resolve arguments before emitting: the runner already accumulates per-call metadata (`startedAtTools`); we add a per-execution stash of `callID → arguments` filled from the assistant message blocks that stream through the frames lane (`agenticToolCalls` already extracts them there — the blocks flow even though the *tool-call start* emission must not double-fire), and the span lane joins from that stash, falling back to empty like today.

Why join at the span lane rather than emit starteds from the message lane and drop spans entirely: the message lane provably doesn't fire for executed calls (the react reroute), so the span lane *is* the emission point; the message lane's job is reduced to feeding the stash. The frames lane also keeps its guarded `emitToolStarted` — if a future provider surfaces tool-call blocks on the live stream, the guard makes whichever lane fires first win and the other becomes a no-op, so the "exactly one started per call id" invariant holds under every lane configuration. Alternative considered: dropping the span lane and emitting purely from a persisted-message replay — rejected; it would delay live tool cards until the model round ends and fight the ADK's event ordering.

### D2: Translator keeps open items in a map keyed by call id

`translate.go` replaces `fcOpen/fcIndex/fcID/fc` with a `map[callID]*openItem` (plus insertion order for stable output indices). `started` opens an item only if absent; `finished` looks up by `ev.ToolResult.CallID` — emitting that call's own `done` (arguments from the started payload, which D1 now fills) and removing the item — so a finished event can never be attributed to a different call and no done is swallowed. Alternative considered: keying by output index only — rejected; it re-creates the mispairing under parallel calls.

### D3: Client card identity is the call id; conversion dedupes as a last resort

Both `onToolCall` handlers change the guard from `args ? find(callId) : undefined` to an unconditional `find(callId)`: if a card exists, update it (set `args` only when the event carries one) and return; push only for an unseen call id. This preserves the added-then-done flow and closes the empty-args hole (which also bites genuinely zero-arg calls). `convertMessage` additionally skips a tool entry whose `call_`-prefixed id was already emitted for the message — defense in depth, since the renderer's resource keying crashes the tree on any duplicate. Alternatives considered: keying client cards by stream item id instead of call id — rejected; the output events address cards by call id, and hydration addresses them by call id too. Relying solely on the conversion dedupe without fixing the handlers — rejected; it would hide live duplicates (two spinning cards) instead of preventing them.

### D4: The regression test pins lane behavior through the real ADK, not a mock seam

The runner test drives a fake agentic model through the real ADK agent with a scripted stream emitting two parallel id-bearing tool calls, then asserts the emitted transcript events: exactly two starteds (with the right arguments) and two finisheds, and — via the existing `/v1` translate test seam — the paired added/done wire items. This is the test that would have caught the original bug and that pins the "frames lane never fires for executed calls" fact: if eino ever changes the react routing, the test tells us which lane fired. Client-side, `runtime.test.ts` replays the malformed wire sequence from the incident (added, added, wrong-call empty-args done, outputs) and asserts one card per call id and no duplicate `toolCallId` in conversion output.

## Risks / Trade-offs

- [Stash may miss arguments for calls whose assistant message is not seen before the span fires] → the join degrades to empty arguments — today's behavior — rather than failing; hydration still repairs on reload. The ordering (message event precedes its spans in the ADK stream, observed in the raw log) makes the miss unlikely.
- [A provider that legitimately reuses a call id across separate executions within one turn] → D2 scopes item state per stream/response; the runner's guard scopes per execution. Cross-execution reuse hits a fresh map either way.
- [Translator behavior change visible to third-party clients] → the change is toward the spec'd contract (one added + one done per call, correct arguments); a client that tolerated the mislabelled done was relying on a bug.
- [The frames-lane stash assumes message blocks arrive before spans] → verified in the raw event log (message at seq N, spans at N+2/N+3); the regression test pins it.

## Migration Plan

Single deploy; no schema or client-config migration. Existing in-flight turns during rollout may still emit the old shape; the client hardening (D3) renders them safely (one card per call id) instead of crashing. Rollback is a plain revert — no data was written in the new shape.

## Open Questions

None. The frames-lane question that gated fix shaping is resolved (spans are the only live source for executed calls on the agentic react path); the GLM stream-tail latency anomaly is explicitly out of scope.
