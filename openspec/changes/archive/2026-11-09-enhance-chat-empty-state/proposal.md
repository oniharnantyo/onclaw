# Proposal

## Why

The chat's empty transcript is a single muted placeholder line ("No messages yet — say hello below."), which reads as unfinished scaffolding on the one surface every new session and showcase screenshot starts from. PRODUCT.md makes first-run impression part of the point (public showcase audience), and the empty thread is the app's front door; it currently tells the reader nothing about who they are about to talk to.

## What Changes

- Replace the empty-transcript placeholder line in `ChatThread`'s intro slot with a centered identity-greeting block: the agent's procedural avatar (`AgentAvatar`), an animated greeting line ("How can I help you today?"), and an identity line weaving the agent's name with the first line of its `Description` (degrading to the name alone when the description is empty).
- Identity resolves through the existing `activeAgentID` → `agents` seam (`ChatHeader` already uses it pre-session), so both a brand-new chat (`selectAgent(agent, null)`) and a fresh session of an existing agent show the same identity.
- Center the block vertically while the thread is empty, using the vendored `thread.aui.tsx` pattern (`min-h-full justify-center` on the transcript column); the loading skeleton stays top-aligned and unchanged.
- Animate avatar → greeting → identity with a small `tw-animate-css` fade/slide stagger (vendored-element idiom), each leg guarded by `motion-reduce:animate-none`.
- Fall back to the generic greeting without identity when no agent is resolvable — group transcripts (where `activeAgentID` is null) keep their identity in the header (name + `GroupAvatarStack`) and are not repeated in the viewport.
- Copy and behavior otherwise unchanged: composer untouched, load-earlier untouched, the no-agents early return in the chat view untouched, settings-pane kit `EmptyState` untouched.

Not in scope: starter-prompt chips, a runtime suggestions adapter, composer changes, transcript-wide motion work.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `chat-view`: ADD an "Empty transcript greeting" requirement — an empty agent thread renders a centered identity greeting (avatar, greeting, identity line) in both themes, with reduced-motion stillness, and the generic fallback when no agent identity applies.

## Impact

- Frontend only: `frontend/src/features/chat/thread/ChatThread.tsx` (intro slot + column centering), possibly a small `thread/ThreadEmptyState.tsx` component, and the empty-state test pin in `frontend/src/features/chat/thread/__test__/chatThread.test.tsx`.
- No backend, bindings, store, or dependency changes; identity fields (`Agent.Name`, `Agent.Description`, `Agent.Avatar`) and `AgentAvatar` already exist.
- Both themes ride existing ported tokens; no new token surface.
