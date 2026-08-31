## Why

At boot the app treats a failed session validation as a *successful* one: when `GET /auth/me` fails with anything other than a 401 (backend down, 5xx, network failure), `boot()` in `web/src/store/auth.ts` sets `status: 'authenticated'` with `user: null`. `RequireAuth` only checks `'unauthenticated'`, so an unauthenticated visitor reaches the full dashboard; the seed store (`db: seedDb()`) renders the Acme demo workspace; `UserMenu` shows identity as "You" (`user?.name || 'You'`). The auth boundary guesses, and the guess is fail-open.

## What Changes

- **Fail-closed boot:** `boot()` gets a third outcome — `status: 'error'` (boot failed, reason preserved) — for any `/auth/me` failure that is not a 401/`unauthenticated` code. `'authenticated'` becomes reachable **only** via a successful `/auth/me`, so the dashboard can never render without a verified user.
- **Boot error page:** a full-screen error surface rendered by `BootGate` when status is `'error'`: explains the server could not be reached / failed, shows the error reason, and offers **Retry**, which sets status back to `loading` and re-runs `boot()` (no page reload).
- **Token preserved on boot error:** the stored `od_token` is kept when boot fails with a server/transport error, so a successful Retry restores the session without re-login. Only a 401 discards the token.
- **Remove the anonymous "You" identity:** delete the `user?.name || 'You'` fallback in `UserMenu` so identity always derives from the verified user; a null user renders a blank identity (unreachable in practice after fail-closed boot — pure defense in depth).
- **Out of scope:** mid-session request failures (already surfaced as toasts per the existing API error envelope), the login form's network-error toast (the login screen is already the correct error surface there), and the larger question of removing seed data from the production store (explored option C — future change).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `web-app/auth`: the "Session boot" requirement gains an explicit error branch — boot failures that are not 401 SHALL route to a boot error page (never the app shell, never /login), with retry re-running boot. The requirement is also tightened to state the dashboard renders only with a verified user (no anonymous identity fallback).

## Impact

- `web/src/store/auth.ts` — `AuthStatus` gains `'error'`; `boot()` else-branch sets it; retry action re-runs boot.
- `web/src/App.tsx` — `BootGate` renders the error page for `'error'`.
- New `web/src/components/BootError` (or similar) — full-screen error surface per the design contract.
- `web/src/components/nav/UserMenu.tsx` — remove the `'You'` fallback.
- Tests: `web/src/store/auth.test.ts` (boot outcomes for network/5xx), BootGate/BootError render tests, UserMenu identity test.
- No backend changes; no API contract changes; no migration.