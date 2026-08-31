## Why

Failures currently have no designed surface: unknown URLs silently redirect to a random chat, a render crash blanks the app (no error boundary exists anywhere), a 5xx on a data load leaves the pane empty, and mid-session network loss spams one toast per failed call. The only designed error state (boot error) is a one-off whose visual grammar (18px vs 24px titles, round vs squircle medallions) has drifted from the three ad-hoc states that copy it. Self-hosted operators get nothing greppable from a user report of "it's broken".

## What Changes

- **ErrorState primitive** — one component for all state pages: unDraw illustration hero (pre-tinted to accent `#2f6feb`, bundled as static SVGs), title/body/actions per the existing grammar, optional mono detail chip. Compact medallion variant for inline contexts.
- **New error pages**
  - Route 404 — unknown URLs render an in-shell 404 state instead of the silent catch-all redirect to a random chat.
  - Bad chat identifier — `/c/:chatId` with a well-formed but missing id renders "This agent no longer exists" instead of silently redirecting.
  - 5xx on a data load — full-page error state (chrome laid down) with Retry re-running the failed load.
  - Render crash — root ErrorBoundary renders a full-page error state instead of a white screen.
- **Connection-lost banner** — one sticky degraded-mode banner (Retry) replaces repeated network toasts while the server is unreachable mid-session; boot-time unreachable keeps the full-page boot error.
- **Boot error unified** — `BootError` refactored onto the primitive with an illustration; the `offline.jpg` photo is removed.
- **Ad-hoc states unified** — workspace-suspended and admin not-authorized states refactored onto the primitive, fixing the title-size and medallion-shape drift.
- **Backend request IDs** — gin middleware issues a request id per request, returns it as `X-Request-ID` and inside the error envelope; the frontend surfaces it in the error chip so self-hosted operators can grep logs.
- **Illustration assets** — ~4 unDraw SVGs (404, 5xx/crash, connection lost, unauthorized/boot) selected as one coherent family, tinted `#2f6feb` at download time.

## Capabilities

### New Capabilities

- `web-app/error-states` — the ErrorState primitive and every full-page / in-shell error page built on it: 404, bad chat id, 5xx, render crash, connection banner surface conventions, illustration treatment, mono detail chip.
- `api-errors` — server-side request id issuance: `X-Request-ID` header on every response, `request_id` in the error envelope, log correlation, and client surfacing rules.

### Modified Capabilities

- `web-app/shell` — Screen routing: unknown URLs render a 404 error state (was: silent redirect to a random chat); a well-formed but unknown `/c/:chatId` renders a not-found state (was: silent redirect to first agent).
- `web-app/auth` — Session boot: the boot error page joins the ErrorState system (illustrated, same grammar). API error envelope: while the server is unreachable mid-session, network failures render one sticky connection banner instead of one toast per failed call.

## Impact

- **Frontend:** `web/src/components/` (new `ErrorState`, `ErrorBoundary`, `ConnectionBanner`), `web/src/App.tsx` (catch-all route, error routes, boundary mounts), `web/src/lib/api.ts` (request-id capture, 5xx classification), `web/src/store/auth.ts` (boot error page), `web/src/screens/` (bad-id state in `ChatRoute`), `web/src/assets/` (SVG illustrations in, `offline.jpg` out).
- **Backend:** `internal/server/` (request-id middleware in the router chain, `request_id` field in the error envelope in `internal/server/errors.go`).
- **API:** additive — new envelope field and response header; no existing field or status changes. Not a breaking change.
- **Assets/licensing:** unDraw illustrations are free for commercial use without attribution; bundled locally so error pages render with zero network.
- **No DB or migration changes.**
