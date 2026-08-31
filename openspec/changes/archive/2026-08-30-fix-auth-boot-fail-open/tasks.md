## 1. Auth store: fail-closed boot

- [x] 1.1 In `web/src/store/auth.ts`, add `'error'` to the `AuthStatus` union and `bootError: string | null` to `AuthState` (initial `null`).
- [x] 1.2 Rewrite the `boot()` catch-else branch: set `{ status: 'error', bootError: formatApiError(err) }` — keep the token, do not touch `user`/`memberships`. Reset `bootError: null` at the start of every `boot()` run and on `login`/`logout`/`clearSession` success paths.
- [x] 1.3 In `web/src/store/auth.test.ts`, cover: network error from `/auth/me` → `status: 'error'` + token still in localStorage; 5xx response → `'error'` + token preserved; 401 → `'unauthenticated'` + token cleared (existing test, keep green); successful `/auth/me` → `'authenticated'` with user/memberships; retry = calling `boot()` again resolves to `'authenticated'` when the API recovers.
- [x] 1.4 Update any existing test that asserts the fail-open behavior (status `authenticated` with null user after network error) to assert `'error'` instead.

## 2. Boot error page

- [x] 2.1 Create `web/src/components/BootError.tsx`: full-screen surface on `bg-bg`, centered card (surface, `border-line`, radius-12, `elev-raised`), shield icon in a danger-tinted circle (reuse the suspended-workspace visual language from `App.tsx:196-216`), title "Couldn't reach OnClaw", body copy stating the session could not be verified, detail line rendering `bootError`, primary **Retry** button (accent, sets `status: 'loading'` then calls `boot()`), secondary **Log in instead** button (clears session, routes to `/login`).
- [x] 2.2 Wire `BootGate` in `web/src/App.tsx` to render `BootError` when `status === 'error'`; `RequireAuth` unchanged (still only redirects on `'unauthenticated'`).
- [x] 2.3 Add `BootError` render tests: error detail line shows `bootError`; Retry re-enters loading state and calls `boot()`; Log in instead clears session and shows /login.

## 3. UserMenu hardening

- [x] 3.1 In `web/src/components/nav/UserMenu.tsx`, change `user?.name || 'You'` to `user?.name ?? ''`.

## 4. Verification

- [x] 4.1 `cd web && pnpm test` — full suite green, including new boot-outcome, BootError, and UserMenu tests.
- [x] 4.2 Manual pass with the backend down: reload the app with a stale token → boot error page (not the shell, not /login); Retry against a recovered backend → straight into the dashboard without re-login.