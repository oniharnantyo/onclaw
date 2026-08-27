# Tasks: scaffold-web-app

Build order follows design.md's migration plan: tokens → UI atoms → nav shell → chat → screens → modals → responsive → parity. Specs live under `specs/web-app/*`.

## 1. Project scaffold & token layer

- [x] 1.1 Scaffold Vite + React + TypeScript app at the `web/` root (`web/Web-Prototype/` untouched); verify dev server runs and serves the prototype at `/Web-Prototype/onclaw-app.html`
- [x] 1.2 Configure `tsconfig.json` to include only `src`; add `web/.gitignore` (`node_modules/`, `dist/`); confirm the production build excludes prototype files
- [x] 1.3 Add Tailwind v4 with `src/styles/tokens.css` (prototype `:root` block verbatim) and `src/styles/theme.css` (`@theme` mapping producing `text-muted`/`border-line`/`bg-warm`/`bg-accent`/`rounded-md` and the mono/sans font utilities)
- [x] 1.4 Self-host fonts via `@fontsource/inter` (400/500/600/700) and `@fontsource/jetbrains-mono` (400/500); confirm Inter body + JetBrains Mono chips render
- [x] 1.5 Port the prototype's base styles and keyframes (`od-scroll`, `od-pop`, `od-fade`, `od-caret`, `od-dot`, `od-live`, selection/focus-visible, range accent) into global CSS

## 2. Data layer & domain types

- [x] 2.1 Create `src/data/types.ts` with the domain vocabulary (Workspace, Agent, Channel, Person, ThreadSession, Message incl. branches/agentId/tools/cron, ToolCall, CronJob, Run, Member, Integration, McpServer, Skill, ApiKey) shaped to the seed objects
- [x] 2.2 Port seed data: both tenants, registries (MODELS/PROVIDERS/SKILLS/MCP_SERVERS/TOOLS/STATUS/COMMANDS), `blankTenant`, reply templates, `withSessions` normalization and `EXTRA_SESSIONS` including the 100-session Ledger list
- [x] 2.3 Port `src/lib` helpers (`cx`, `uid`, `slugify`, `fmtUses`, `nowTime`, `memberHandle`, `parseMentions`, `providerOf`, `craftReply`)
- [x] 2.4 Build the Zustand store with narrow selectors (`useWorkspace`, `useThread`, `useSessions`, `useRuns`, `useTenant`) and all actions (send, refreshMessage, branchNav, editSubmit, session lifecycle, upsertAgent, cron actions, workspace create/switch/delete, tenant patching, toasts)
- [x] 2.5 Implement the simulated runtime behind the store (typing delay, streaming-caret id, staggered mention replies, `runNow` running→success transition) with cleanup-safe timers
- [x] 2.6 Implement UI-position persistence (workspace, route, chat, members-panel state) in localStorage and the Cmd/Ctrl+K search-focus handler

## 3. UI atoms (`src/components/ui`)

- [x] 3.1 Port Icon (with the prototype's inline icon set), Avatar, StatusDot, Chip
- [x] 3.2 Port MentionText, Toggle, Toasts, Modal (including `wide` and footer slot)
- [x] 3.3 Port Segmented, OptionChips, MicroLabel, ViewShell, SideRow, SectionLabel, LastRunCell
- [x] 3.4 Port the shared form styling constants (`inputCls`, `labelCls`) used by modals and panes

## 4. Navigation shell & routing

- [x] 4.1 Set up React Router with the five routes and redirects (unknown `/c/:chatId` → first agent; zero-agent workspace → `/welcome`).
- [x] 4.2 Port `Rail` (view icons with unread badge, workspace avatar, settings entry) wired to route navigation.
- [x] 4.3 Port `Sidebar` (search, agents list with status dots, channels with unread, teammates, cron/runs rows, inline session list for the active agent chat with new/switch/delete).
- [x] 4.4 Port `WorkspaceSwitcher` popover (plan badges, create entry) with switch semantics (reset to first agent chat, toast).
- [x] 4.5 Build `NavDrawer`: sidebar content off-canvas below 768px with a rail-visible trigger, focus handling, and dismissal; static sidebar ≥768 (prototype's `md` threshold).

## 5. Chat screen (`/c/:chatId`)
- [x] 5.1 Port ChatHeader (channel/agent/person variants, status + last-active line, member avatars stack with overflow count, configure button)
- [x] 5.2 Port message components: AgentMessage, UserMessage (inline edit), OtherMessage, ThinkingRow, ToolCall card (name/args/latency, expandable), CronChip, BranchPicker
- [x] 5.3 Port transcript windowing and scroll semantics (80-message window, load-earlier +400, user-message top pinning, at-bottom tracking, floating jump-to-bottom) and the empty state with starter suggestions
- [x] 5.4 Port Composer: autosizing textarea, send/stop swap, attach stub toast, SlashMenu and MentionMenu with arrow-key navigation, Enter/Esc handling, focus retention on pick
- [x] 5.5 Wire send routing: person DM (no reply), `/reset` clear, channel mention routing (staggered per-mentioned-agent), primary-agent fallback, scripted `/tools` `/model` `/help` `/schedule` replies with cron-editor auto-open
- [x] 5.6 Wire branching (regenerate appends variant, picker navigation), edit-and-resubmit truncation, session title derivation
- [x] 5.7 Port ContextPanel as static column ≥1280 and slide-over sheet below (add/remove members, candidates list, primary-agent protection)

## 6. Remaining screens
- [x] 6.1 Port AgentsView + AgentCard (status semantics, error notice, responsive 1/2/3-column grid, empty state)
- [x] 6.2 Port CronView table (expression + human label, next/last run, pause/resume toggle, run-now with live run insertion, empty state)
- [x] 6.3 Port RunsView (seven-column table, All/Succeeded/Failed filters, empty and no-match states)
- [x] 6.4 Port OnboardingPane as the `/welcome` screen (deploy-first-agent CTA, settings path, plan/timezone footer)

## 7. Modals
- [x] 7.1 Port AgentConfigModal: two-column structured form (identity/provider/model cascade/temperature slider/role/system prompt; tools/skills/MCP chips; autonomy segmented; channel-post toggle), validation, deploy→open-chat and edit-merge flows, workspace-skill merging into skill options
- [x] 7.2 Port CronEditorModal (agent select, read-only timezone, expression validation with inline error, human label, enabled switch, delete for existing)
- [x] 7.3 Port CreateWorkspaceModal (name→slug derivation with conflict validation, timezone, starter-agent choice) wired to `blankTenant` + switch-on-create
- [x] 7.4 Port SettingsModal shell with the seven-tab rail; inline panes: workspace (save + two-click danger zone with 4s window and last-workspace guard), members (invite/role/remove, immutable owner), integrations (connect/disconnect)
- [x] 7.5 Port McpPane (add server, pause/reconnect, retry on error, expandable tool chips, agent-usage counts) and SkillsPane (install custom, toggle, run/usage counts)
- [x] 7.6 Port keys and notifications panes: create/reveal/copy/revoke with clipboard-failure toasts; draft-until-save toggles + routing email

## 8. Responsive fill-ins & shell hardening
- [x] 8.1 Reflow RunsView and CronView rows into stacked cards below 768px preserving all cell content (no horizontal overflow at 360px)
- [x] 8.2 Make modals full-height sheets below 768px; set the app shell to `100dvh` so the composer survives mobile keyboards
- [x] 8.3 Sweep every screen across the contract viewport matrix (360/390/430/600/820/1024/1366/1440/1920) confirming zero horizontal overflow and correct nav states

## 9. Visual parity loop & wrap-up

- [x] 9.1 Add the Playwright parity script: screenshot app vs `/Web-Prototype/onclaw-app.html` at the nine viewports for chat/agents/cron/runs screens, emitting a side-by-side report
- [x] 9.2 Run the parity loop and fix structural/token drift until screens match the prototype side-by-side
- [x] 9.3 Update `CLAUDE.md` Commands with the web dev/build/test commands; final `vite build` + typecheck clean, no console errors on any route
