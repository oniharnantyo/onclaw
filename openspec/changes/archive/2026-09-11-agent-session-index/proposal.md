## Why

The per-agent chat session index lives only in the browser (`localStorage["onclaw.threads.v1"]`): transcripts are durable server-side in `session_events`, but the client is the sole holder of "which sessions exist for this agent" — there is no server API to list them. A second browser, another machine, incognito, or cleared site data silently loses the entire sidebar history while the transcripts remain stranded in Postgres, unreachable because their `sess_<uuid>` addresses are unknown.

## What Changes

- New `agent_sessions` table (migration 000041): the server-side session index — workspace, agent, owning user, session id, title, soft-delete marker, birth and last-activity timestamps.
- The runner registers every persistent (non-ephemeral) turn into the index at run start: first turn births the row and derives the title from the user input (first line, trimmed, 42 chars + ellipsis — the same rule the web uses today); later turns only bump `last_active_at`.
- New endpoint `GET /agents/:agent/sessions` listing the requesting user's sessions for the agent, newest activity first, each row carrying a `running` flag merged from the run manager's live-run registry.
- New endpoint `DELETE /agents/:agent/sessions/:session` performing a **soft delete** (sets `deleted_at`; transcripts stay on disk and are recoverable).
- Run manager gains the ability to enumerate its live run keys for a workspace so the list endpoint can mark running sessions.
- The web sidebar sources each agent's session list from the server instead of treating localStorage as truth (localStorage stays as a fast-path cache). Refetch triggers: own turn completion (stream EOF), window focus/visibility, sidebar expand, and the cross-tab `storage` event.
- Running indicator on sidebar session rows: the row shows a leading spinner and its title pulses while the session has a live run — merging the local run state (instant) with the server `running` flag (other tabs/devices); idle rows carry no marker.
- No backfill: `session_events` never recorded agent attribution, so sessions created before this change cannot be indexed retroactively; everything post-deploy indexes itself. Existing browsers keep their history via the localStorage cache.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: new requirement — durable per-user session index written at run start, with workspace/agent/user-scoped listing (including live-run status) and soft deletion.
- `web-app/chat`: session list rendering is MODIFIED — the sidebar sources sessions from the server list endpoint (server is source of truth, localStorage demoted to cache) and ADDED requirement — per-session running indicator with local/server state merge.

## Impact

- **Backend:** `migrations/` (000041 up/down), `internal/agents` (runner upsert on run start; run-manager enumeration), `internal/store` (+ postgres adapter: new `AgentSessionStore` sub-interface), `internal/server/handlers` (list + delete endpoints), `internal/server/router.go` (routes, permission guards), `internal/store/fake` (test fake).
- **Web:** `web/src/lib/api.ts` (list/delete calls), `web/src/store` (server-sourced list, overlay demotion, refetch triggers), `web/src/components/nav/Sidebar.tsx` (row indicator), `web/src/lib/livechat.ts` / runtime (dot state source).
- **API surface:** two new workspace-scoped endpoints under the agents group, gated by existing `agents:read` / `agents:write` permissions.
- **No breaking wire changes:** existing `/v1` session binding, hydration, and cancel semantics are untouched; the index is additive.
