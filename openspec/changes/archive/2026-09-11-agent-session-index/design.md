## Context

`session_events` (migration 000016) persists transcripts keyed by `(workspace_id, session_id)` with no agent or user attribution, so the server cannot answer "which sessions exist for this agent". The web fills that gap with `localStorage["onclaw.threads.v1"]` (`web/src/store/threadPersistence.ts`), making the sidebar per-browser: incognito, a second browser, or cleared site data shows an empty history while the transcripts sit unreachable in Postgres. Channels already solved the same problem server-side (`channel_work_sessions` + `GET /channels/:id/sessions`). Live runs are tracked in memory by `runManager` keyed `{WorkspaceID, AgentID, SessionID}` (`internal/agents/runmanager.go`), and transcript hydration already exists (`GET /agents/:agent/sessions/:session/events`).

User-locked decisions from exploration: **private per user**, **pulsing-dot indicator (mockup A)**, **soft delete**, **no backfill**.

## Goals / Non-Goals

**Goals:**
- Session index durable server-side; sidebar history follows the account, not the browser.
- Last-activity ordering so rechatting an old session bumps it to the top.
- Per-session running indicator combining local (instant) and server (cross-tab/device) state.
- Title derived server-side on birth with the web's existing rule so optimistic and authoritative titles agree.

**Non-Goals:**
- No backfill of pre-index sessions (nothing to attribute them with).
- No rename/edit-title feature (a later `PATCH` can ride the same table).
- No workspace-level live event feed for foreign-run dots (focus-driven refresh is enough for v1; the channels hub is the precedent if wanted later).
- No tightening of direct session-id access — ownership governs the *list*; transcript hydration and run binding stay permission-scoped (documented behavior, unchanged).
- No trash/restore UI — soft delete only lays the foundation.

## Decisions

**D1 — Dedicated `agent_sessions` table, not derivation from `session_events`.** `session_events` has no agent/user columns; deriving would require attribution we never persisted, and grouping scans per listing are the wrong cost profile. Mirror of `channel_work_sessions`.

```sql
CREATE TABLE agent_sessions (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    agent_id       uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id     text NOT NULL,
    title          text NOT NULL DEFAULT '',
    deleted_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_active_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id, session_id)
);
CREATE INDEX idx_agent_sessions_user_listing
    ON agent_sessions(workspace_id, agent_id, user_id, last_active_at DESC);
```

Surrogate `id` PK with a unique triple keeps the store's uuid-id convention (matches `channel_work_sessions`); listing queries filter `deleted_at IS NULL`. Alternative rejected: `(workspace_id, session_id)` natural PK — sessions are client-minted `sess_<uuid>` so collisions are negligible either way, but the unique triple is stricter and free.

**D2 — Index write at run start inside the runner.** The runner already resolves user, agent, and input before `runManager.start`; it upserts once per persistent run:

```sql
INSERT ... (workspace_id, agent_id, user_id, session_id, title, last_active_at)
ON CONFLICT (workspace_id, agent_id, session_id) DO UPDATE
  SET last_active_at = now(), deleted_at = NULL,
      title = CASE WHEN agent_sessions.title = '' THEN EXCLUDED.title
                   ELSE agent_sessions.title END
```

Title = first line of `ExecRequest.Input`, trimmed, ≤42 chars + ellipsis — byte-identical rule to the web's `pushMsg` (`store/index.ts:236-238`), so the optimistic client title and the server title agree by construction. The `CASE` guard makes birth the only title-writing turn; `/compact` (whose input is summarizer focus text) can never retitle. `deleted_at = NULL` on conflict un-deletes a session rechatteed after a soft delete — the transcript is still on disk, so reviving is correct. Bump at run **start**, not completion, to match the client's local ordering behavior and give instant feedback. Ephemeral runs (`RunEphemeral`) skip the upsert entirely. Store access rides a new `store.AgentSessionStore` sub-interface injected into the runner (positional param, per DI conventions); a failed upsert logs a warning and does NOT fail the run — indexing is bookkeeping, not execution.

**D3 — Listing endpoint merged with the live-run registry.** `GET /api/workspaces/:slug/agents/:agent/sessions` (gated `agents:read`) returns `{sessions: [{id, session_id, title, created_at, last_active_at, running}]}` for the **requesting user**, `deleted_at IS NULL`, `ORDER BY last_active_at DESC`. `running` comes from a new `runManager.ActiveRunSessionIDs(workspaceID, agentID)` (iterate the `live` map under its mutex, collect session ids) intersected with the rows — one map read, no per-row queries. `DELETE /api/workspaces/:slug/agents/:agent/sessions/:session` (gated `agents:write`) sets `deleted_at = now()` on the caller's own row; 404 for foreign-owned rows (indistinguishable from absent — no existence leak).

**D4 — Web: server is source of truth, localStorage demoted to cache.** On agent chat open, render immediately from the local overlay (today's behavior — no blank flash), then fetch the list and reconcile: server rows win for the index (order, titles, running, soft-deletes dropped), purely-local sessions (pre-index history) are appended below server rows so nothing the user still sees disappears. `last_active_at` orders the list; the client maps server rows to local thread state by `session_id` binding. Refetch triggers: own turn completion (stream EOF / terminal event — where the dot also stops), window `focus`/visibilitychange, sidebar expand ("Show older sessions"), and the `storage` event fired in other tabs when tab A writes `onclaw.threads.v1`. No polling timer in v1.

**D5 — Running indicator: pulsing title (user-locked after implementation pass).** Original mockup A (idle hollow dot / pulsing accent dot) shipped, and the user found the per-row idle dots noisy ("the dot seems weird — pulse the history when running"). Final form (further revised after the user found the pulse alone too subtle): **no marker on idle rows**; while running, the row shows a leading border spinner (the ConnectionBanner idiom: `h-3 w-3 animate-spin rounded-full border border-current border-t-transparent`) before the title AND the title text breathes with the app's standard `animate-pulse` opacity idiom, plus a "run in progress" hint in the native tooltip. State merge unchanged: `localRunActive(session) || server.running`, exactly one row animates per running session, and either source going terminal stops the pulse.

**D6 — Delete wiring.** Sidebar delete → `DELETE` endpoint first, then local removal (existing "last session spawns a fresh empty session" rule unchanged). If the deleted session was active, activate an adjacent session. Soft delete keeps `session_events`/checkpoints intact — recovery stays possible and a future trash UI is a listing filter away.

## Risks / Trade-offs

- [No backfill — pre-index sessions invisible cross-device] → Accepted (user-locked): old sessions remain on the browsers that made them via the local overlay; all post-deploy turns index themselves. Documented in-app copy is unnecessary; behavior is self-explanatory.
- [Foreign-run dot staleness between refreshes] → Accepted for v1: focus/visibility/turn-end refetches cover real glance patterns. A workspace-level feed (channels-hub style) is the v2 escape hatch.
- [Run-start upsert adds one write to every turn's latency path] → Negligible: single indexed upsert executed before the run registers; failure path logs and continues without failing the run (D2).
- [Optimistic-vs-server title divergence] → Both sides share one truncation rule; divergence only if the rule drifts. A shared constant is impossible (Go/TS), so the design pins the rule in both spec scenarios and tests (title rule test in both languages).
- [Soft-deleted session re-run via direct id resurrects the row] → Intended: `deleted_at = NULL` on conflict (D2). Listing is the only privacy boundary; transcripts were never deleted anyway.

## Migration Plan

Single migration `000041_agent_sessions` (up/down pair, down drops the table). No data migration. Deploy order is trivially safe: the table is additive, the endpoints are new, the old web build ignores them. Rollback = revert web + drop table. Dev DB is at v40 (verify with `migrate status` before applying — channel-teams landed 000040).

## Open Questions

None — all product and visual decisions were locked during exploration (private-per-user, mockup A, soft delete, no backfill).
