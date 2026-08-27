# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What OnClaw Is

OnClaw is a multi-tenant, self-hosted **AI agent workspace** — an OpenClaw / Hermes alternative. Each tenant gets a **workspace** (e.g. "Acme Corp") where members create named **agents** (Atlas, Beacon, …) configured with a system prompt, provider/model, temperature, exposed tools, skills, slash commands, and MCP servers. Agents are reached through direct chats, team **channels** (`#ops`, `#incidents`), and **cron schedules** (e.g. a morning digest); executions appear as **runs** with tool-call cards (`grafana.query`, `files.write`, …) in the transcript.

## Fixed Stack & Requirements

- **Backend:** Go (module `github.com/oniharnantyo/onclaw`, Go 1.27).
- **Database:** PostgreSQL.
- **Frontend:** Vite + React + Tailwind CSS, built from the design contract below.
- **Multi-tenant:** workspaces are the tenant boundary; members have Owner/Admin/Member roles. Tenant isolation is enforced at the data layer — no query runs without a workspace scope.
- **Extensibility via interfaces:** backend extension points (providers, tools, channels, storage, …) are Go interfaces; implementations register into a registry. Built-ins ship as ordinary registrations, not special cases.
- **Plugins are first-class:** plugins target the _same_ interface contracts as built-ins. If a feature cannot be added without editing core code, the interface is wrong — fix the interface.

## Repository State

Greenfield — **not yet a git repository**. `go.mod` exists with no dependencies; there is no backend code, no Vite scaffold, and no migrations yet. When scaffolding lands, update Commands below and `git init` if still missing.

### Commands

```bash
go build ./...                      # build backend (works once packages exist)
go test ./...                       # all tests
go test -run TestName ./internal/foo/...   # single test
go vet ./...

# Web Frontend Commands
cd web
npm run dev                         # start Vite dev server
npm run build                       # typecheck and build for production
npm run preview                     # preview production build
npm run test:e2e                    # run Playwright visual parity tests
```

Integration tests will need PostgreSQL — prefer a dockerized instance for local dev.

## Frontend Design Contract

`web/Web-Prototype/` is the **visual source of truth** (exported via the Hallmark skill; `skills-lock.json` pins it): `onclaw-app.html` (single-file React prototype), `DESIGN-MANIFEST.json` (machine-readable screen/token/interaction map), `DESIGN-HANDOFF.md` (the binding contract), `onclaw-preview.png`. The production app must reproduce this design, not reinterpret it. Non-negotiables:

- **Extract tokens before writing components.** The prototype defines CSS custom properties — accent `#2f6feb`, bg `#fafafa`, surface `#ffffff`, border `#e5e5e5`, fg `#111111`, muted `#6b6b6b`; Inter (body/display) + JetBrains Mono; radius 8/12/16/pill; motion 150/200ms `cubic-bezier(0.2, 0, 0, 1)`. Never substitute framework-default themes or typography.
- **Screen-file-first:** each screen is its own route — chat, agents, cron/schedules, runs, workspace settings (members, skills, MCP, API keys), onboarding/workspace creation.
- **Responsive:** must work from 360×800 to 1920×1080 with zero horizontal overflow (full viewport matrix in `DESIGN-HANDOFF.md`).
- **Structured config forms, never raw JSON textareas.** One labeled field per config property — see the prototype's agent config modal (provider select, model, temperature, system prompt, tool toggles, thread retention). Read-only pretty-printed JSON is fine for _display_; textareas only for genuinely unstructured values (system prompts, scripts).
- **Preserve states and patterns:** hover/focus/loading/empty/error/success states, real copy (no marketing filler), monospace chips for models/tools, tool-call cards with latency in transcripts, mention (`@agent`) and slash-command menus in the composer.

## Domain Vocabulary

Use these names consistently across schema, API, and UI (they come from the prototype):

- **Workspace** — the tenant: name, URL slug, plan, timezone. Switchable via the workspace switcher; created through onboarding (optionally with a starter agent).
- **Member** — workspace user with role Owner / Admin / Member.
- **Agent** — persona + config: system prompt, provider, model, temperature, tools exposed, skills, slash commands, MCP servers, thread retention.
- **Chat / Thread** — conversation with an agent; transcript includes tool-call cards and cron-origin markers.
- **Channel** — team room (`#ops`, `#incidents`); **Team** — direct messages with members.
- **Cron / Schedule** — name, expression, next/last run, trigger, email routing.
- **Run** — one agent execution: status/state, tokens used, started/last-active.
- **Skills / MCP servers / Tools** — agent-attachable capabilities.
- **API keys / Tokens** — per-workspace credentials.
