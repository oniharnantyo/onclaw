# Tasks — adopt-assistant-ui-chat

## 1. Setup

- [x] 1.1 Install `@assistant-ui/react` (pinned to the 0.15.x line) and peer dependencies in `web/`; verify `npm run build` and `oxlint` stay green on the untouched app first.
- [x] 1.2 Record the shipping bundle delta (chat route chunk before/after install) in the change notes. (Bundle size changed from ~60KB to 156KB gzip; single chunk without code-splitting).

## 2. Runtime bridge (`web/src/chat/runtime.tsx`)

- [x] 2.1 Implement `convertMessage`: OnClaw message → `ThreadMessageLike`, carrying agent identity, cron-origin, and tool invocations as metadata/parts; tool invocations map to tool-call parts with running/completed states.
- [x] 2.2 Introduce bridge-owned running state (`ui.running`) and derive `isRunning` from it; remove `ui.typing`/`ui.streamId` from components' concerns.
- [x] 2.3 Implement `onNew`: append user message, run slash-command handling (`/reset` clears session; `/tools`/`/model`/`/help` scripted replies; `/schedule` reply + editor open), teammate-DM no-turn path, channel mention fan-out (staggered, per-agent), then agent turn.
- [x] 2.4 Implement chunked response engine: buffer reply chunks (~40ms flush) appended incrementally into the active session; thinking/running indicator driven by running state; `/reset` clears without an agent turn.
- [x] 2.5 Implement `onCancel`, `onEdit` (truncate + re-run), `onReload` (new variant via branch state), branch navigation mapping the mock's variant arrays to runtime branch state (branch picking enabled in agent DMs).
- [x] 2.6 Reset bridge state on session switch/delete/new so no stale running state survives a session change.

## 3. Component migration (runtime handlers)

- [x] 3.1 Mount `AssistantRuntimeProvider` + bridge around `ChatView` (chat route only).
- [x] 3.2 Wire transcript rendering to bridge handlers while retaining component props.
- [x] 3.3 Wire `BranchPicker` to runtime branch state (`n / total`, prev/next), DM-only.
- [x] 3.4 Wire `Composer` submit/stop states to runtime handlers.
- [x] 3.5 Gate edit/reload/branch affordances to agent DMs via handler checks.

## 4. Store cleanup

- [x] 4.1 Delete superseded store actions `respondFor`, `refreshMessage`, `branchNav`, `editSubmit` and their state flags (`typing`, `streamId`) after bridge parity is confirmed.
- [x] 4.2 Remove now-unused constants/templates imports and update remaining call sites (`App.tsx` prop drilling of `onRefresh`/`onBranch`/`onEditSubmit`).

## 5. Verification

- [x] 5.1 **Test bridge translation paths**. Unit test the converter (`web/src/chat/runtime.tsx`):
  - [x] Message properties round-trip cleanly (id, role, text).
  - [x] Agent mention paths fan out accurately.
  - [x] Cancellation interrupts and keeps partial text, DM-only affordance gating.
- [x] 5.2 Run Playwright parity suite; only streaming-arrival and tool-card-running-state baselines may change (both spec-sanctioned).
- [x] 5.3 `npm run build`, oxlint, and `openspec validate --strict` all green.
