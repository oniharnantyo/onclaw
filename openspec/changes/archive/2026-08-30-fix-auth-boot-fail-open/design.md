## Context

`web/src/store/auth.ts` boot() has three branches today: no token → `unauthenticated`; `/auth/me` 200 → `authenticated`; 401 → `unauthenticated` + token cleared. The catch-all else sets `authenticated` with `user: null` — the fail-open bug. `web/src/lib/api.ts` already classifies transport failures as `ApiError(0, 'network')`, and 5xx responses surface as `ApiError(status, code, message)`, so the error branch already has the information it needs — it just guesses wrong.

## Goals / Non-Goals

**Goals:**
- Boot failure that is not a 401 lands on a dedicated error surface, never the app shell and never /login.
- Retry from the error page re-runs the same boot routine (status → `loading` → boot()), no page reload, no duplicated boot logic.
- Preserve the stored token on boot error so retry restores the session without re-login.

**Non-Goals:**
- Mid-session request failures (toasts per the existing API error envelope — unchanged).
- Login-form network errors (the login screen is already the right surface; toast + disabled submit already exist).
- Removing seed data from the production store (explored option C — future change).
- Auto-retry with backoff. Retry is manual in v1.

## Decisions

**D1. New `AuthStatus` value `'error'`** — added to the existing `'loading' | 'authenticated' | 'unauthenticated'` union. Reusing `'unauthenticated'` was rejected: that is exactly the conflation that caused the bug (transport failure ≡ no session). A separate value keeps each state's surface distinct: `loading` → spinner, `unauthenticated` → `/login`, `error` → error page.

**D1a. Boot-error state carries the failure reason.** The store keeps `bootError: string | null` alongside status, populated from `formatApiError(err)` at the failure site. The error page renders it as the detail line. Alternative considered: re-derive the message in the component by keeping the raw error object in state — rejected; storing a rendered string avoids resurrecting stale errors after successful retry.

**D2. BootGate renders the error page.** The gate owns the status→surface mapping: `loading` → spinner, `error` → error page, else children. Putting the error surface inside `RequireAuth` was rejected — RequireAuth's single concern is `unauthenticated` → `/login`; adding a second condition there re-couples the two failure modes the change separates. The error page is a sibling of the loading screen in `BootGate`, full-screen per the design contract.

**D3. Retry re-runs `boot()`.** The Retry control dispatches `boot()`; status must be reset to `loading` first so the spinner shows during the attempt. No `window.location.reload()` — a reload would discard nothing (token is in localStorage) but re-mounts the whole tree and re-seeds `db` from `seedDb()`, which this change deliberately avoids feeding unauthenticated traffic. No separate `refresh()` action; `boot()` is the one boot routine.

**D3a. Secondary "Log in instead" escape hatch.** The error page offers a secondary action that calls `clearSession()` (discards token) and routes to `/login`. Without it, a permanently-down instance leaves a stale-token visitor in a retry dead end. Small scope addition — cut it if you want the page leaner.

**D4. UserMenu fallback removal.** `user?.name || 'You'` → `user?.name ?? ''`. After fail-closed boot, a null user cannot reach the shell, so this is dead code in practice — pure defense in depth. Alternative: disable the menu when user is null; rejected as unreachable, not worth the branch.

## Risks / Trade-offs

- [Transient backend blip at deploy → users now see an error page instead of silently browsing the demo dashboard] → Intended behavior; the error page explains itself and Retry recovers automatically once the server responds.
- [Retry hammering a struggling server] → Manual retry only in v1; if needed later, add backoff behind the same boot() entrypoint.
- [bootError string could go stale] → Cleared (set to null) on every boot() run and on successful login/logout/clearSession.
- [403 forbidden (disabled user) at boot now lands on the error page] → Accepted: the error page renders the server's reason via formatApiError and offers Log in instead. A dedicated disabled-account surface is out of scope.
- [Seed data still in the store for authenticated users] → Not touched by this change; de-seeding is the future option-C change.

## Migration Plan

Pure frontend, no migrations, no API changes. Rollback = revert the deploy. No data risk.

## Open Questions

(none)