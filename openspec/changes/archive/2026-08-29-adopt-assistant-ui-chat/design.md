# Design — adopt-assistant-ui-chat

## Context

The web app is a visual-parity scaffold over a zustand mock: `web/src/store/index.ts` fakes agent turns with timers and template replies, and the chat components (`web/src/components/chat/**`) hand-roll streaming, branching, editing, and cancelation on top of it. There is no backend. The prototype (`web/Web-Prototype/`) is the binding pixel contract, enforced by a Playwright visual-parity suite.

## Goals / Non-Goals

**Goals:**
- Replace hand-rolled turn logic with a library-owned interaction layer (streaming, running state, cancel, edit, reload, branch navigation).
- Keep the design contract intact: headless primitives only, zero styled-component leakage.
- Isolate transport behind one module so the future Go/Responses backend change touches handlers only.

**Non-Goals:**
- Go backend, `/v1/responses` wire contract, real SSE — separate later change; this change's specs deliberately avoid transport detail.
- Server-side threads, sessions, or persistence.
- Reworking non-chat screens; the runtime provider wraps the chat route only.

## Decisions

### D1 — ExternalStoreRuntime, not LocalRuntime or DataStream
`useExternalStoreRuntime` keeps zustand as the single source of truth. Alternatives rejected:
- *LocalRuntime*: runtime owns thread state (split brain with the workspace store), single-assistant turn model conflicts with multi-agent channels, and its adapter contract assumes resending full history.
- *DataStream*: wrong wire grammar and it is LocalRuntime underneath anyway.
Also decisive: external pushes (a cron run completing while the user watches another screen) mutate the store directly and the runtime reflects them with no back-door into library internals.

### D2 — Runtime-routed handlers + store-derived props
Components keep their current markup/classes and still consume state via props driven from the store/bridge. Handlers route through the library's `chatRuntime`. Rationale: Migrating deeply nested legacy components to headless primitives en masse poses a high regression risk for a visual scaffold. Instead, behavior routes via the bridge while view components remain decoupled.

### D3 — The bridge is the transport seam
New module `web/src/chat/runtime.tsx`:
- `convertMessage`: OnClaw `Message` → `ThreadMessageLike`, carrying agent identity, cron-origin, and tool invocations as message parts/metadata. Tool invocations become tool-call parts so a card can render running → completed.
- Handlers `onNew`/`onEdit`/`onReload`/`onCancel`/`onAddToolResult` implement turns by driving the existing mock engine (timers, template replies, mention fan-out, slash commands) and mutate the store via existing actions (`pushMsg`, `updateTenant`) — with `respondFor`/`refreshMessage`/`branchNav`/`editSubmit` logic **moved into the bridge**, then deleted from the store.
- `isRunning` derives from a bridge-owned `ui.running` flag (replaces `typing` + `streamId`).

### D4 — Mock streaming becomes chunked, not instant
The response engine now appends text in buffered chunks (≈40ms flush) so the transcript shows incremental arrival — the behavior assistant-ui primitives key on — instead of "whole message + caret".

### D5 — DM-only behavior scoping
Edit/reload/branch controls render only in agent DMs. Channel turns have multi-author semantics nobody has defined; faking a policy now would be throwaway. Revisit when the backend orchestrates channel orchestration.

### D6 — Dependency hygiene
`@assistant-ui/react` pinned to the 0.15.x line (pre-1.0 churn expected; upgrade deliberately, not transitively). It internally uses zustand (same major as ours — dedupes) and zod v4; radix primitives ride as transitive deps. Not yet code-split (single 542KB / 156KB-gzip chunk); this is an acceptable deviation for the current scaffold phase.

## Risks / Trade-offs

- [Pre-1.0 churn] → Pin exact-ish, upgrade deliberately; all library surface lives in one bridge module + primitive call sites, keeping the blast radius small.
- [Per-token store updates re-render the transcript] → Buffer chunks (~40ms), memoize message components, keep selectors component-scoped.
- [Visual parity breakage during migration] → Migrate one behavior at a time, running the parity suite after each; baselines only re-baseline for the two explicitly-allowed spec deltas (streaming arrival, tool-card running state).
- [Branch model mismatch] → The mock's `branches[]` array is shallower than runtime branch trees; bridge maps it locally until real backend defines the model.
- [Corrupted file writes during this change's authoring] → Read back and validate each artifact (`openspec validate`) before finishing.

## Open Questions

- Exact `onclaw:*` metadata inventory (what rides on messages beyond agentId/cron/tools) — deferrable; converter can grow fields additively.
- Whether branch picking ships enabled in v1 or is stubbed — deferred until the bridge maps the mock's variant model to runtime branch state; decide during implementation.
- Session tabs: sessions stay store-level (orthogonal to the runtime); how session switching resets runtime state is settled during implementation.
