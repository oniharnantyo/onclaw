# Tasks

## 1. Empty-state component

- [x] 1.1 Create `frontend/src/features/chat/thread/ThreadEmptyState.tsx` rendering the centered block: `AgentAvatar` (size 56), greeting line "How can I help you today?", identity line `With {name}` plus ` — {first non-empty description line}` when one exists (single line, truncated), with the `tw-animate-css` stagger (avatar/greeting/identity at 0/150/300ms, `fill-mode-both`, `motion-reduce:animate-none` per leg) and a greeting-only fallback (no avatar, no identity line) when the agent prop is null; verify with `cd frontend && npx tsc --noEmit`
- [x] 1.2 Wire `ThreadIntro`'s empty branch in `ChatThread.tsx` to render `ThreadEmptyState`, resolving the agent from `activeAgentID` → `agents` (the `ChatHeader` seam); verify the empty branch no longer renders the placeholder `<p>` and `npx tsc --noEmit` stays clean
- [x] 1.3 Apply the centering pattern: while the thread is empty and not loading, give `TranscriptLog` `min-h-full justify-center` and skip the always-empty messages wrapper so flex gap cannot add phantom space; verify the skeleton stays top-aligned and `npx vitest run src/features/chat/thread/__test__/chatThread.test.tsx` passes with updated pins

## 2. Tests and live check

- [x] 2.1 Update `chatThread.test.tsx`: replace the placeholder-text pin (line ~520) with pins for identity-greeting content (avatar present, name + description first line), empty-description degradation (name alone), group fallback (no identity line), reduced-motion class presence, and skeleton-before-greeting ordering; verify with `npx vitest run src/features/chat/thread/__test__/chatThread.test.tsx`
- [x] 2.2 Full gate: `cd frontend && npx tsc --noEmit && npx vitest run` and confirm the new pins pass alongside the existing suite; then `task dev` and eyeball a fresh chat (avatar + greeting centered, both themes), an empty-description agent, and a fresh group (generic greeting) — reduced-motion via OS setting *(automated gate green: tsc clean, vitest 142 files / 1499 tests; the `task dev` theme/reduced-motion eyeball remains owed to the user)*
