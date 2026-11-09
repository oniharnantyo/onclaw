# Design

## Context

`ChatThread` renders the transcript on assistant-ui primitives; its `ThreadIntro` slot shows skeleton rows while `loadingSession`, otherwise a single muted `<p>` ("No messages yet — say hello below.") when the thread is empty (`frontend/src/features/chat/thread/ChatThread.tsx:92-122`). The transcript column (`TranscriptLog`) is a `max-w-3xl` flex column inside the scroll viewport, sized to its content — nothing centers vertically today. The vendored reference `frontend/src/components/assistant-ui/elements/thread.aui.tsx` carries the idioms this design borrows: a `ThreadWelcome` greeting line with `tw-animate-css` entrance classes, and the `isEmpty && "justify-center"` column pattern — both deliberately unmounted so far. Identity for the pre-session state already flows through the store's `activeAgentID` → `agents` seam (`selectAgent(agent, null)` sets `activeAgentID` without minting a session; `ChatHeader` falls back to it). `AgentAvatar` (procedural face from `Agent.Avatar`, id-derived fallback) and `tw-animate-css` are already in use. Groups open with `activeAgentID: null` (`store.ts:1407`) and carry their identity in the header (name + `GroupAvatarStack`). One test pins the placeholder copy (`chatThread.test.tsx:520-523`).

## Goals / Non-Goals

**Goals:**
- A centered, animated identity greeting on the empty agent transcript, correct in both themes and under reduced motion.
- Reuse existing seams only: store identity, `AgentAvatar`, ported tokens, `tw-animate-css`.

**Non-Goals:**
- Starter-prompt chips or a runtime suggestions adapter (the linked EmptyState element's suggestion/composer parts are not vendored).
- Any composer, header, skeleton, or no-agents-view change.
- Transcript-wide motion work (the parked `animate-chat-chrome` direction stays separate).

## Decisions

- **D1 — Dedicated component; intro slot keeps its branches.** The block becomes `thread/ThreadEmptyState.tsx`; `ThreadIntro` keeps the skeleton branch and renders the new component in its empty branch. ChatThread stays a composition shell; the block's markup (avatar, two lines, stagger classes) would bloat it otherwise. Alternative — inline in `ThreadIntro` — rejected for the same size reason.
- **D2 — Identity from `activeAgentID` → `agents`, same as `ChatHeader`.** It is the only identity that exists on a brand-new chat (no session yet, so `Session.AgentID` is unavailable) and it is already the header's pre-session source, guaranteeing header and greeting agree. Groups and any unresolvable agent render the greeting-only fallback (no avatar, no identity line) — the header already owns group identity, and repeating the roster in the viewport is redundant.
- **D3 — Centering via the vendored column pattern.** While the transcript is empty and not loading, `TranscriptLog` gains `min-h-full justify-center`; the percentage resolves against the definite-height viewport, and an empty thread never scrolls, so the block lands mid-viewport. The messages wrapper (whose children all return null while empty) is skipped while the intro shows, so flex `gap` does not add phantom space around a zero-height box. The skeleton stays top-aligned — a skeleton that grows downward reads as loading, not welcome.
- **D4 — Stagger via `tw-animate-css` utilities, not a vendored element.** Avatar/greeting/identity get `animate-in fade-in slide-in-from-bottom-1/2 fill-mode-both` with 0/150/300ms delays and `motion-reduce:animate-none` per leg — the vendored `ThreadWelcome` idiom, three utility classes per leg. Vendoring the doc's full EmptyState element family would drag in composer and suggestion parts buddies does not use.
- **D5 — Copy.** Greeting: "How can I help you today?" (vendored parity). Identity line: `With {name}`, plus ` — {first description line}` when a description exists, single line truncated. Time-aware greetings skipped: clock state and hydration concerns for marginal warmth.
- **D6 — Tests pin behavior, not animation.** `chatThread.test.tsx` swaps the placeholder pin for: identity greeting content (avatar present, name + description first line), the empty-description degradation, the group fallback (no identity), reduced-motion class presence, and the skeleton-before-greeting ordering. Animations do not run in jsdom; presence and class assertions are the contract.

## Risks / Trade-offs

- [Centering drift if the viewport's height context changes] → `min-h-full` resolves against the same viewport `ChatThread` already sizes with `h-full min-h-0 flex-1`; a regression shows immediately as a top-hugging block in the one surface everyone opens first.
- [Greeting flash on session switch] → the `loadingSession` guard stays ahead of the empty branch (existing ordering, now pinned by the "loading history never flashes" scenario).
- [Description first line is long or blank-with-whitespace] → trim, take the first non-empty line, truncate to one line; degradation to name-only is itself a spec scenario.
- [jsdom environment quirks] → no new environment dependencies: `ResizeObserver` is already stubbed in this file, and the block uses no observers or portals.

## Migration Plan

Frontend-only, no data or bindings. Ship behind nothing; revert the commit to roll back. `frontend/bindings` regen is unnecessary (no Go change).

## Open Questions

None — the greeting and identity copy are review-level tweaks that do not move the approach or the spec.
