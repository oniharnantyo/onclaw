# Proposal

## Why

Sessions created outside the web composer — external API clients naming their own sessions via `metadata.onclaw_session` (the OpenResponses contract), scheduler tooling, or benchmark harnesses — appear in the session list but render an empty transcript when opened: the web only hydrates history for session ids matching its own `sess_`/`sched_` prefixes. The 2026-10-03 harness-bench run surfaced this live (50+ bench sessions with rich transcripts, all unreadable in the pane). Additionally, hydration loads the entire event log unbounded, which grows linearly with session age.

## What Changes

- Transcript hydration becomes **server-authoritative for every server-known session**: the `sess_`/`sched_` id-prefix gate is removed. Any session id hydrates on open; unknown sessions fail soft (local thread untouched).
- Initial hydration loads only the **most recent window** of events (bounded page) instead of the whole log.
- **Infinite scroll-up pagination**: scrolling to the top of the transcript fetches the next older page via a new `before` cursor on the session-events endpoint and prepends it with scroll position preserved.
- Server: the session-events read endpoint gains backward-pagination parameters (`before`, `limit`, has-more cursor). Forward `after` semantics are unchanged. The event store's existing reverse keyset support is exposed through `History` and the HTTP layer.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `web-app/chat-runtime` — Transcript hydration requirement broadened to all server-known sessions; windowed initial hydration and scroll-up paging added.
- `agent-session-events` — Load semantics gain the `before` cursor and backward-window reads alongside the existing forward `after` cursor.
