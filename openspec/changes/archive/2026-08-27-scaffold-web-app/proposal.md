# Proposal: scaffold-web-app

## Why

OnClaw has a binding visual contract — the single-file React prototype in `web/Web-Prototype/` — but no production frontend. The prototype is itself a React 18 + Tailwind app (~45 components, seeded multi-tenant data, simulated agent runtime), so the production build is an *extraction* into a real Vite project, not a reinterpretation. Scaffolding now unblocks all frontend work while the Go backend is still greenfield: screens ship against seed data behind a swappable data seam, and the future API replaces that seam without touching components.

## What Changes

- New Vite + React + TypeScript app at the `web/` root (Vite root = `web/`), with `web/Web-Prototype/` kept untouched as the design source of truth and reachable in dev for side-by-side pixel comparison.
- Tailwind v4 with the prototype's `:root` tokens ported verbatim into `tokens.css` and mapped to utilities via `@theme` (accent/line/warm/muted/etc. generate the same class names the prototype uses). Fonts self-hosted via `@fontsource` (Inter, JetBrains Mono).
- React Router with five routes: `/c/:chatId` (agent, channel, or teammate DM in one flat namespace), `/agents`, `/cron`, `/runs`, `/welcome` (zero-agent workspace onboarding). Active workspace (tenant) lives in the store + localStorage, not the URL.
- All ~45 prototype components ported 1:1 into `components/ui`, `components/nav`, `components/chat`, `screens/`, `modals/`, preserving copy, states (hover/focus/loading/empty/error/success), tool-call cards with latency, mention and slash-command menus, and the structured (never raw-JSON) agent config form.
- Mock data layer: Zustand store holding seeded workspaces (2 tenants, 6 agents, channels, threads with sessions, crons, runs, members, skills, MCP servers, keys), plus a simulated agent runtime (typing delay, scripted slash-command replies, streaming caret, branch/regenerate, mention routing in channels, live run-status transitions).
- Responsive behavior: the prototype's own thresholds are preserved (sidebar static ≥768, channel members panel static ≥1280); below them the sidebar becomes an off-canvas drawer and the members panel a slide-over sheet (the prototype merely hides both). Runs/cron tables become stacked cards below 768. The app must pass the handoff's nine-viewport matrix (360×800 → 1920×1080) with zero horizontal overflow.
- Playwright-based visual parity loop: screenshot prototype vs. app at the nine contract viewports.

No backend changes (none exists yet); no modifications to `web/Web-Prototype/`.

## Capabilities

### New Capabilities

- `web-app/shell`: app scaffold, design tokens, routing, workspace-switcher persistence, and responsive navigation (rail, sidebar, off-canvas drawer, viewport thresholds).
- `web-app/chat`: chat screen — transcript with tool-call cards and cron-origin markers, composer with slash/mention menus, per-agent session lists, channel members panel, simulated agent responses including branching and edit-and-resubmit.
- `web-app/agents`: agents screen (cards grid) and the structured agent configuration modal (identity, provider/model, temperature, tools, skills, MCP, autonomy, channel posting).
- `web-app/schedules`: cron screen (schedule table with pause/resume/run-now) and the schedule editor modal.
- `web-app/runs`: run history screen with status filters and the seven-column run table.
- `web-app/settings`: the seven-tab workspace settings modal (workspace, members, integrations, MCP servers, skills, API keys, notifications) and its danger zone.
- `web-app/workspaces`: workspace lifecycle — switcher, creation with optional starter agent, deletion with last-workspace guard, and the zero-agent onboarding screen.
- `web-app/data-layer`: the in-memory workspace store, seed data, UI-position persistence rules, and the seam contract the future Go API must plug into.

### Modified Capabilities

(none — greenfield, no existing specs)

## Impact

- **Code**: everything under `web/` except `web/Web-Prototype/` (new `package.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `src/**`). `go.mod` and future backend code untouched.
- **Dependencies (runtime)**: react, react-dom, react-router, zustand, tailwindcss v4, `@fontsource/inter`, `@fontsource/jetbrains-mono`. **Dev**: vite, typescript, `@vitejs/plugin-react`, playwright (parity checks).
- **Tooling/config**: `web/tsconfig.json` must include only `src` so the compiler never processes the prototype; Vite's dev server intentionally also serves `/Web-Prototype/…` (parity tab). `.gitignore` under `web/` for `node_modules/`, `dist/` (repo is not yet a git repository).
- **Docs**: CLAUDE.md Commands section gains the web dev/build commands once the scaffold lands.
- **Risk**: the prototype has no tests and zero automation; the Playwright parity loop is the acceptance gate for the visual contract.
