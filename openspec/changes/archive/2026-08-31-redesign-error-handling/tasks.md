## 1. Backend: request ids (`api-errors`)

- [x] 1.1 Add request-id middleware in `internal/server`: issue a 16-char hex id per request (echo inbound `X-Request-ID` when well-formed: non-empty, ≤64 chars, `[A-Za-z0-9-]`), set `X-Request-ID` on every response, stash in gin context; register it first in the router chain in `internal/server/router.go`
- [x] 1.2 Extend `internal/server/errors.go`: `RespondError`/`AbortWithError` read the request id from the gin context and include `request_id` in the error envelope; error log lines carry the id as a slog field
- [x] 1.3 Update `ErrorEnvelope`/`APIError` JSON tests for the new field; add middleware tests (header on 200 and 500, envelope/header parity, inbound echo validation)
- [x] 1.4 Backend tests green: `go vet ./... && go test ./...`

## 2. Assets: illustrations

- [x] 2.1 Download 4 unDraw illustrations (not-found, server-error, connection, unauthorized) pre-tinted to `#2f6feb` from undraw.co; one coherent family; save as static SVGs in `web/src/assets/`
- [x] 2.2 Remove `web/src/assets/offline.jpg` and its import from `BootError`

## 3. Frontend primitives

- [x] 3.1 Create `web/src/components/ErrorState.tsx`: single component, `full` variant (illustration hero: w-48/sm:w-64 slot, od-fade entry) and `compact` variant (icon medallion); grammar per design D1 (24px semibold title, 14px muted body, primary/secondary actions, mono detail chip)
- [x] 3.2 Create `web/src/components/ErrorBoundary.tsx`: class boundary; `full` mode (Reload = `location.reload()`) and `shell` mode (in-shell state, reset by subtree remount via key bump)
- [x] 3.3 Create `web/src/store/connection.ts` (zustand): network failures from `api.request` set degraded; any success resets; `web/src/components/ConnectionBanner.tsx` renders sticky banner + Retry when degraded
- [x] 3.4 In `web/src/lib/api.ts`: capture `request_id` from the error envelope into `ApiError.requestId`; classify status-0 as network (existing); suppress 'network' toasts while degraded (banner replaces them)

## 4. Routes and views

- [x] 4.1 `web/src/App.tsx`: replace the `*` catch-all redirect with an in-shell 404 ErrorState route; mount root ErrorBoundary around `BrowserRouter` tree and shell ErrorBoundary around `<main>`'s `<Routes>`
- [x] 4.2 `ChatRoute`: well-formed but unknown chat id → not-found ErrorState (View agents / Back to chats); malformed/empty id keeps the redirect to first agent
- [x] 4.3 View data-load error path: view store slices gain `loadError` (set by loaders); 5xx renders the full-page ErrorState with in-place Retry; status 0 keeps loading/empty (banner territory); mutation failures keep toasts
- [x] 4.4 Mount `ConnectionBanner` in `Layout`
- [x] 4.5 Update existing state pages onto the shared grammar: BootError (full variant, keeps behavior contract: token preserved, Retry re-runs boot, "Log in instead" clears session), workspace-suspended, admin not-authorized, OnboardingPane (compact or full per fit)

## 5. Tests and verification

- [x] 5.1 Unit tests: ErrorState variants render + grammar classes; ErrorBoundary catches and recovers; ConnectionBanner appears/suppresses toasts/recovers
- [x] 5.2 Route tests: unknown URL renders 404 state (no redirect); bad chat id renders not-found; malformed id still redirects
- [x] 5.3 Update `web/src/App.test.tsx` and `BootError.test.tsx` for the new presentation; run `pnpm test`, `pnpm build` green
- [x] 5.4 Full verification: `go vet ./... && go test ./...`, `cd web && pnpm test && pnpm build`, `./scripts/smoke.sh` green; manual pass over 404/5xx/crash/banner states
