# Design: scaffold-web-app

## Context

`web/Web-Prototype/onclaw-app.html` is a 3,003-line single-file React 18 + Tailwind (CDN/Babel) application: ~45 hook-based components, an inline `tailwind.config` mapping `:root` CSS variables to utilities, seeded multi-tenant data, and a simulated agent runtime. The production build is therefore an *extraction* into a real Vite project, not a reimplementation — JSX, class names, and behaviors port nearly 1:1. The Go backend does not exist yet, so the app runs on seed data behind a seam. The handoff (`DESIGN-HANDOFF.md`) makes the prototype a pixel contract and mandates a nine-viewport responsive matrix with zero horizontal overflow; the prototype itself contains five breakpoint rules (sidebar `md:flex`, members panel `xl:flex`, agents grid `sm:/xl:`, config modal `md:`) and hides — rather than adapts — the sidebar and members panel below their thresholds.

## Goals / Non-Goals

**Goals:**

- Byte-faithful token layer and 1:1 component port, verifiable by screenshot comparison against the prototype.
- A data layer that the future Go API can replace without touching screen or component code.
- Responsive behavior that preserves the prototype's own thresholds and fills the gaps (drawer, sheet, table cards) without inventing new visual language.
- Machine-checkable visual parity at the nine contract viewports.

**Non-Goals:**

- Real backend integration, auth, or multi-user behavior.
- Dark mode (token aliases like `--surface-warm` leave room; nothing builds on it now).
- Real streaming, tool execution, or LLM calls — the simulated runtime is the contract until the API lands.
- Unit/integration test suites beyond the parity loop.
- List virtualization libraries; the 100-session seed is a stress case, handled by selector-level subscriptions first.
- Settings deep-linking (modals stay ephemeral overlays, not routes).

## Decisions

### D1 — Vite root at `web/`, prototype co-located
Vite's dev server serves the prototype at `/Web-Prototype/onclaw-app.html`, giving a same-origin parity tab for free. Guardrails: `tsconfig.json` includes only `src`; nothing ever imports from `Web-Prototype/`; `web/.gitignore` excludes `node_modules/` and `dist/`. *Alternative:* `web/app/` subdir — rejected as needless nesting given the dev-server benefit.

### D2 — TypeScript, domain types as the future API contract
`src/data/types.ts` holds the CLAUDE.md domain vocabulary (Workspace, Agent, Channel, Person, ThreadSession, Message, ToolCall, CronJob, Run, Member, Integration, McpServer, Skill, ApiKey) shaped exactly like the seed objects, so they later mirror the Go API structs. *Alternative:* JSX-only for a faster 1:1 port — rejected: the data seam is where type safety pays off at API time.

### D3 — Tailwind v4 with tokens verbatim + `@theme` mapping
`src/styles/tokens.css` is the prototype's `:root` block pasted verbatim (fidelity contract). `src/styles/theme.css` maps each variable to a v4 theme namespace entry (`--color-accent: var(--accent)`, `--radius-md`, `--font-sans/mono`), generating the same utility names the prototype's classes already use (`text-muted`, `border-line`, `bg-warm`, `rounded-md`). Arbitrary values like `bg-[color-mix(in_oklab,var(--fg)_9%,transparent)]` port unchanged (underscore→space conversion is identical in v3 CDN and v4). *Alternatives:* v3 with JS config (older, config indirection), CSS modules (loses the utility vocabulary the port relies on).

### D4 — React Router, five flat routes, tenant in store only
`/c/:chatId` (heterogeneous agent/channel/DM namespace resolved from data, matching the prototype's resolution order), `/agents`, `/cron`, `/runs`, `/welcome`. Unknown chat id redirects to the first agent; zero-agent workspace routes to `/welcome`. The active workspace lives in the store + localStorage (as the prototype's `pos` does), so switching workspace keeps the current route and swaps the data underneath. *Trade-off accepted:* URLs are not tenant-qualified — sharing a link opens the recipient's last workspace. Mitigation: workspace resolution stays inside one hook so a path-prefix retrofit (`/w/:ws/…`) touches one layer. *Alternative rejected:* path-prefixed tenancy now — cleaner deep links but heavier router and speculative before the API exists.

### D5 — Zustand store as the mock-era state layer
One store holds the workspace map, active workspace id, and ephemeral UI flags (typing, streaming id, open modals, toasts). Screens subscribe via narrow selectors (`useThread(chatId)`, `useSessions(agentId)`, `useRuns()`), so a transcript update does not re-render the 100-session sidebar list. Actions: `send`, `refreshMessage`, `branchNav`, `editSubmit`, `newSession`/`switchSession`/`deleteSession`, `upsertAgent`, `saveCron`/`toggleCron`/`deleteCron`/`runNow`, workspace create/switch/delete, tenant-patching settings updates. At API time this store is replaced by a query cache behind the same hooks (see `web-app/data-layer` spec). *Alternatives:* context + reducer (re-render granularity problem), Redux (ceremony for a layer slated for replacement).

### D6 — Responsive: prototype thresholds + drawer/sheet fill-ins
Sidebar static ≥768 and members panel static ≥1280 exactly as the prototype's own wrappers decree; below those, a `NavDrawer` (the only component with no prototype counterpart) hosts the sidebar off-canvas with a rail-visible trigger `<md`, and the members panel becomes a slide-over sheet toggled by the existing header control. Runs/cron tables reflow to stacked cards below 768 (their fixed pixel grids would otherwise clip). App shell uses `100dvh` for keyboard survival; modals become full-height sheets below 768. *Alternative rejected:* drawer at `<1024` — overrides a threshold the prototype already set; fidelity contract says preserve present layout rules.

### D7 — Hand-rolled chat primitives (assistant-ui semantics, custom pixels)
The prototype's comments cite assistant-ui behaviors (turnAnchor top-pinning, ThreadList, edit-and-resubmit, ActionBar reload); the code hand-rolls all of them. Port them as-is rather than adopting the library, whose styling would fight the pixel contract. Preserve: 80-message window + load-earlier (+400), user-message top pinning, floating jump-to-bottom (>36px threshold), branch variants stored on the message with an index pointer, truncate-and-rerun on edit. Drop the legacy bare-array thread shape handling (`getThreadState`'s migration branch) — seed normalization (`withSessions`) runs once at boot in the port.

### D8 — Fonts self-hosted via `@fontsource`
Identical glyphs to the prototype's Google Fonts link without a runtime CDN dependency, fitting the self-hosted product. Loaded weights mirror the prototype's (Inter 400/500/600/700, JetBrains Mono 400/500).

### D9 — Playwright parity loop
A dev-only script screenshots the app and the prototype (served by the same dev server, D1) at the nine contract viewports for the primary screens, emitting a side-by-side report. This is the acceptance gate for the visual contract; it intentionally does not assert pixel equality (font rendering varies) but catches structural/token drift. *Alternative:* manual eyeballing — rejected as unrepeatable.

## Risks / Trade-offs

- **CDN Tailwind → v4 build drift** (subtle arbitrary-value or preflight differences) → build the token/theme layer first and run the parity loop on a skeleton before porting 45 components.
- **React 19 vs prototype's React 18** (dev-mode double-invoked effects) → the simulated runtime's timers must be idempotent/cleanup-safe; verify typing/caret flows early.
- **Tenant-blind URLs** → keep workspace resolution in a single hook for a later path-prefix retrofit.
- **Pixel drift across a 45-component port** → tokens frozen before components; largest-regions-first build order; parity loop at each screen milestone.
- **Sidebar re-renders on transcript growth** → zustand selectors from day one; virtualization deferred (CSS `content-visibility` as a cheap first lever if the 100-session list stutters).
- **No behavioral unit tests in scope** → type safety + parity loop are the gate; behavior specs remain as the future test backlog.

## Migration Plan

Greenfield — no existing production frontend to migrate. Rollback is deleting the scaffold (`web/{src,index.html,package.json,…}`); the prototype is never modified. Implementation sequence (also the task order): tokens/theme → UI atoms → nav shell + routing → chat screen → remaining screens → modals → responsive fill-ins → parity loop.

## Open Questions

- Exact drawer-trigger placement within the rail (`<md` only) — lean: a list icon in the rail's top cluster; movable later without touching routes or the store.
- Toast duration and stacking exactness — port the prototype's ~3s auto-dismiss first; tune later.
- Whether the small inline settings panes (workspace/members/integrations/keys/notifications) split into separate files — split when any pane grows, not preemptively.
