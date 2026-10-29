# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

(Web-technology design language throughout: React + Tailwind in the Wails3
desktop webview, and the same UI served over HTTP/WebSocket in server mode.
There is no per-OS native design-language split.)

## Users

Primary user: the developer/owner running buddies as a local single-user
gateway — the one "human" each agent knows. Their daily jobs span four modes,
all confirmed active today:

- coding / project work (repo tasks, code research, PR work in chat);
- knowledge & notes (reading, summarizing, corpus and long-term memory);
- life admin & comms (scheduling, reminders, messaging via Telegram/triggers);
- unattended automation (cron/webhook-fired turns checked after the fact).

Secondary audience (confirmed): the open-source public. buddies is a public
showcase — first-run impression and clean architecture are part of the point,
so future visitors/readers evaluate the repo and its screenshots even though
the runtime is single-user.

## Product Purpose

buddies hosts configurable AI agents on the eino runtime as a local gateway:
a desktop app (window + system tray) that keeps running with the window
closed, or a headless server on loopback. Everything persists locally in
SQLite plus plain Markdown workspaces — no external services required.
Success means an agent that knows you well enough that you trust it with
unattended reach: it remembers, it acts on triggers while you're away, and
you can read exactly what it knows and did.

## Positioning

Companions and reach as one mechanism, neither alone: named personas with
avatars, per-agent durable memory, and an owner profile ("your human") are
what make always-on triggers — cron, webhooks, chat — trustworthy enough to
leave unsupervised. A generic agent runner can copy a tray gateway or a
chat persona; the fused claim — "it knows you, so you can trust it running
while you're away, and everything it knows is plain files on your disk" —
is the position. Local-first, legible data (SQLite WAL + Markdown
workspaces under `~/.buddies`) is inseparable from that trust.

## Operating Context

- **Desktop (primary):** macOS window + tray. Closing the window hides it;
  the gateway keeps running; tray Quit is the only full exit. Tray offers
  Open and gateway pause/resume.
- **Server mode:** same app headless on loopback (default `127.0.0.1:8080`),
  optional bearer token; Linux build and Docker image are GUI-free.
- **Main surfaces:** Chat (grouped session rail + streaming transcript,
  context usage, compaction), Agents (cards + full config incl. avatars,
  memory, MCP), Settings (providers, skills, tools, hooks, MCP servers,
  integrations catalog).
- **Data:** `~/.buddies/` — `buddies.db` (SQLite WAL), `skills/`,
  `workspaces/<slug>/memory/` (curated index + fact notes + daily notes +
  archive), `logs/`.
- **Feature work goes through the OpenSpec workflow** (`openspec/`), not
  ad-hoc changes; commands are Task (`task dev` / `build` / `run` /
  `build:server`); verification is `gofmt`/`go vet`/`go test -race` plus
  frontend `npm run test` and `npm run build`.

## Capabilities and Constraints

**Confirmed capabilities:** event-sourced sessions with streaming and
compaction; per-agent config (persona, model, tools, skills, MCP, hooks);
per-agent file-backed memory with lexical recall and an hourly consolidation
sweep; knowledge corpus and create_document generation with turn artifacts;
skills registry (`$skill` invocation, shadowable per agent); subagents;
approvals; browser tool; MCP servers with OAuth and an integrations catalog;
scheduler (cron triggers); light/dark theming.

**Constraints:** local-only and single-user — no accounts, no multi-tenant
mode, no external service ever required; UI is offline-capable
(self-hosted fonts); server binds loopback unless explicitly told otherwise.

**Open/undecided (do not invent):** trigger reach beyond the scheduler —
webhooks and Telegram chat are the stated direction (AGENTS.md), not yet
built. Distribution details (packaging, docs site) for the public audience
are undecided.

## Brand Commitments

- Name: **buddies** (rebranded from `onclaw-v2`).
- Voice: informal "buddies-voice" copy — agents are individuals with names,
  avatars, and a relationship to their human, not anonymous tools.
- The locked visual contract lives in DESIGN.md (design system, not
  restated here); Hallmark operates there as discipline, not design source.

## Evidence on Hand

- The shipped app itself (desktop + server) and a detailed README.
- `openspec/` — complete spec set and change archive documenting every
  shipped capability.
- Live smoke evidence under `.team/smoke/` (screenshots, PDFs).
- No external testimonials, press, benchmarks, or user research exist;
  none may be fabricated.

## Product Principles

1. **Companions earn reach.** Memory and persona are what make unattended
   triggers trustworthy; design the companionship and the reach together,
   never as separate features.
2. **Local and legible.** Everything on-device, in plain files a human can
   read; nothing requires an external service.
3. **One human, first-person agents.** A single owner per instance; agents
   relate to "their human", not to accounts or tenants.
4. **Always-on by default.** Closing the window is not quitting; the
   gateway keeps serving triggers until the tray says quit.
5. **Showcase-grade craft.** As a public portfolio piece, first-run
   impression, clean architecture, and verified behavior are product
   features, not overhead.

## Accessibility & Inclusion

No product-specific user needs established beyond standard desktop
accessibility. The design contract already commits to: OS
`prefers-reduced-motion` and `prefers-reduced-transparency` honored
globally, a single instant `:focus-visible` voice, and full keyboard
operation of kit primitives — preserve these in all future work.
