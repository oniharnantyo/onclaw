## Context

The app has one designed error state (boot error) and three ad-hoc copies of its grammar (suspended workspace, admin not-authorized, onboarding) that have drifted in sizing. There is no 404 page, no render-crash boundary, no view-level 5xx handling, and network failures mid-session toast once per failed call. React Router is used in declarative mode (`<Routes>`), which has no built-in `errorElement`. The backend error envelope (`{error: {code, message, details}}`) carries no request id, so self-hosted operators cannot correlate a user-reported failure with server logs. An in-flight change, `fix-auth-boot-fail-open`, also touches the boot error page.

## Goals / Non-Goals

**Goals:**
- One shared state-page component and grammar for every failure (and the onboarding empty state).
- Every failure class gets a designed surface: 404, bad chat id, 5xx data load, render crash, mid-session network loss, boot failure.
- Self-hosted operators can correlate user-reported errors via request ids.
- Zero-network rendering: illustrations bundled, boot page works offline.

**Non-Goals:**
- Migrating React Router to data routers (`createBrowserRouter`) — declarative `<Routes>` stays.
- Error-tracking service integration (Sentry et al.).
- Offline outbox / retry queues for failed mutations.
- Any dark-mode, i18n, or empty-state redesign beyond onboarding adopting the shared grammar.

## Decisions

### D1. One shared `ErrorState` primitive, two variants
Every state page renders through one component: **full** variant = bundled illustration hero + grammar; **compact** variant = icon medallion for inline contexts. The illustration hero uses unDraw SVGs **pre-tinted at download time** to accent `#2f6feb`, bundled as static assets (like `offline.jpg` today) — no svgr plugin, no runtime tinting, renders with zero network.

**Alternatives considered:** medallion-only (safe, least distinctive); mono-status hero (new visual device, contract stretch); runtime-tinted SVG components (needs svgr plugin for little gain); remote URLs (fails exactly when needed).

### D2. Structure matrix: in-shell vs full-page
- **In-shell (chrome stays):** route 404, 403 walls, bad chat id, onboarding.
- **Full-page (chrome replaced):** 5xx on view data loads, render crash, boot failure.
- **Banner (no takeover):** mid-session network loss.

Rationale (user-selected): severity signals via layout; 4xx are navigational (user keeps context), 5xx are ours to fix. Because a single flaky 500 on one data load now takes down the shell, Retry re-runs the load **in place** (no reload) and mutation 5xx stays a toast.

### D3. Bad chat id → state, with a malformed-id carve-out
A well-formed but unknown `/c/:chatId` renders the not-found state. A malformed id (or empty) keeps redirecting to the first agent — that's a typo/malformed URL, not a "gone resource". The previous behavior (silent redirect for all unknown ids) was spec'd in `web-app/shell` Screen routing; the delta modifies it.

### D4. Request ids via gin middleware
Middleware placed first in the router chain (`internal/server/router.go`): generate 16-byte crypto/rand hex (16 chars) per request — no new dependency; echo inbound `X-Request-ID` if well-formed (non-empty, ≤64 chars, `[A-Za-z0-9-]`) for distributed tracing. It sets the response header on every response and stashes the id in the gin context; `RespondError`/`AbortWithError` read it when building the envelope's `request_id` and error log lines include it (`slog` field). Success bodies unchanged — id rides the header only.

**Alternatives:** UUID library (new dep for no behavioral gain); server-generated only (fine, but tracing across services is nicer with inbound passthrough).

### D5. Connection banner via a small connection store
`api.request` network failures (status 0) flip a tiny zustand `connection` store to degraded; any successful response resets it. The banner renders in `Layout` when degraded (fixed top, sticky, accent-border, Retry = no-op health check or just hides on next success). While degraded, the toast action suppresses 'network' toasts (the banner replaces them); boot path is untouched — unreachable at boot keeps the full-page boot error page.

**Alternatives:** window online/offline events only (browser events lag and miss server-down-with-uplink); toast throttling (hides information without adding recovery affordance).

### D6. Crash boundaries: root + shell, no router migration
Hand-rolled class `ErrorBoundary` (declarative routing has no `errorElement`). Root boundary wraps the whole app (full-page variant, Reload = `location.reload()`); a second boundary inside `Layout` wraps `<main>`'s `<Routes>` so a crashing view keeps the rail/sidebar; that one renders the in-shell state and resets by remounting the subtree via a key bump. Crash states show the error message in the mono chip.

### D7. View data-load error path
Views currently swallow load failures. Each view container's store slice gains a `loadError: ApiError | null`, set by the loader; screens render the **full-page** `ErrorState` when `loadError.status >= 500`, and treat status 0 (network) as banner territory (keep loading/empty state). Retry clears `loadError` and re-invokes the loader.

### D8. Migration of existing states
`BootError`, workspace-suspended, admin not-authorized, and `OnboardingPane` adopt the shared grammar (fixes the 18px/24px and medallion-shape drift). `offline.jpg` and its import are removed. `BootError` keeps its behavior contract (token preserved, Retry re-runs boot, "Log in instead" clears session) — only its presentation changes.

## Risks / Trade-offs

- [In-flight `fix-auth-boot-fail-open` also touches `BootError`] → Sequence: land this change's BootError work after (or rebase onto) that change's implementation; the behavior contract stays intact, only presentation is refactored.
- [A single flaky 500 on one data load takes down the shell] → Retry re-runs the load in place; mutation 5xx stays a toast; no auto-retry loop.
- [Envelope field addition — client compat] → Additive only; `ApiError` parses `request_id` optionally; older clients ignore it.
- [Middleware ordering] → Request-id middleware MUST run before routing/recovery so panic responses carry ids; verified in `router.go` chain order.
- [Illustrations add bundle weight] → unDraw SVGs are small (~5–15KB each); total added <100KB.
- [Full-page 5xx could mask a recovering server] → The detail chip (`500 · internal · req_…`) plus in-place Retry gives users/operators enough signal to recover without a reload.

## Migration Plan

Single deploy; frontend and backend are independently backward-compatible (envelope field additive; frontend tolerates absent `request_id`). Rollback is a plain revert of both sides. The request-id middleware ships with tests asserting header + envelope parity. BootError refactor sequences after `fix-auth-boot-fail-open` lands (or rebases onto it).

## Open Questions

- Exact unDraw illustrations (which files) — pick at implementation from one family; any coherent set satisfies the specs.
