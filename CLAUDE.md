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
- **Injected dependencies are never nil:** the composition root (`internal/cli` → `internal/server/router.go`) resolves every dependency — including built-in defaults — before constructing services and handlers. Code at the point of use assumes non-nil and MUST NOT add `if x != nil` defensive guards on injected dependencies; nil checks are reserved for optional request payloads, optional response data, and errors.
- **Explicit dependency injection, no fat config structs:** constructors take the granular dependencies they use as positional parameters — one param per store sub-interface (`agents store.AgentStore`, `users store.UserStore`, …) or narrow interface — never a whole `store.Store` aggregate and never a kitchen-sink config struct bundling many dependencies. Defaultable behavior knobs are functional options (`WithToolRegistry`, `WithSummarizationMargin`). The single exception is transaction-bound code: methods that call `store.WithTx` span multiple sub-stores atomically and keep the whole `store.Store` aggregate, documented as the required transaction seam.

## Repository State

Backend scaffolded — Go service implementing identity, multi-tenancy, and master-tenant control plane using `gin` (HTTP), `pgx/v5` (PostgreSQL), `urfave/cli/v3` (CLI), `golang-migrate` (embedded SQL schema migrations), and `golang-jwt/v5` (JWT HS256 auth). Local storage adapter supports atomic file capability key serving.

### Codebase Layout

```
main.go             Root entrypoint: builds root command via internal/cli, runs, and exits
internal/
  cli/              Composition root: command definitions (server, migrate, user, superadmin) & driver blank imports (drivers.go)
  config/           Configuration struct & per-command assembly (flags > environment variables > defaults)
  domain/           Entities (User, Workspace, Role, Member), validation rules, error sentinels, permission catalog & algebra
  auth/             Authentication ports & registry, password provider (argon2id), TokenIssuer (JWT HS256), auth Service
  server/           HTTP router, error translation envelopes, middleware (auth, workspace context, permission guards, master tenant context), REST API handlers
  server/handlers/  Endpoint handlers (auth, workspaces, members, roles, users, capability file serving, instance admin)
  store/            Data storage ports (Users, Workspaces, Roles, Members) & in-memory test fake
  store/postgres/   PostgreSQL store adapter using pgxpool & embedded SQL migrations
  storage/          File/blob storage port & in-memory test fake
  storage/local/    Local filesystem storage driver under data-dir with atomic writes & capability URLs
  bootstrap/        Fresh-instance lifecycle: EnsureMaster (master tenant) + SeedSuperadmin (initial superadmin account)
migrations/         000001_users … 000006_workspace_master (up/down SQL migration pairs)
scripts/
  smoke.sh          End-to-end curl-based smoke test suite
```

### Commands

```bash
# Build & Verification
go build ./...                                      # build all backend packages
go vet ./...                                        # vet code
go test ./...                                       # run unit and fake-based tests
go test -tags=integration ./...                    # run PostgreSQL integration tests
./scripts/smoke.sh                                  # run full end-to-end curl smoke test suite

# Database Migrations
go run . migrate up [--database-url <dsn>]          # apply all pending migrations
go run . migrate down [--steps N] [--all]           # rollback migrations
go run . migrate status                             # check current migration version
go run . migrate version                            # print migration version

# Server
go run . server [--listen-addr :8080]               # start HTTP API server
# Environment variables for server:
#   DATABASE_URL (required)
#   ONCLAW_ENCRYPTION_KEY (required; 32-byte hex or base64, generate with openssl rand -hex 32; key rotation requires re-entering keys)
#   ONCLAW_LISTEN_ADDR (default :8080)
#   ONCLAW_JWT_SECRET (HS256 key; ephemeral if unset)
#   ONCLAW_DATA_DIR (default ./data)
#   ONCLAW_DIR (root for OnClaw runtime files; default $HOME/.onclaw; workspace root is $ONCLAW_DIR/workspaces)
#   ONCLAW_SUPERADMIN_EMAIL (initial seed email)
#   ONCLAW_SUPERADMIN_PASSWORD (initial seed password)
#   .env in the working directory is auto-loaded before flags resolve; real env vars win over .env

# User & Superadmin Management
go run . superadmin create --email <e> --name <n> --password <p>  # create or seed superadmin in master tenant
go run . user create --email <e> --name <n> --password <p>        # create user account [--avatar-file <path>]
go run . user list                                                # list all user accounts
go run . user disable --email <e>                                 # disable a user account

# Web Frontend Commands
cd web
pnpm install                        # install dependencies
pnpm dev                            # start Vite dev server (proxies /api to the backend)
pnpm build                          # typecheck and build for production
pnpm preview                        # preview production build
pnpm test                           # run vitest unit tests
pnpm test:e2e                       # run Playwright visual parity tests
```

Integration tests require PostgreSQL — set `DATABASE_URL` or `TEST_DATABASE_URL` (e.g. `postgres://localhost:5432/postgres?sslmode=disable`).


## Frontend Design Contract

`web/Web-Prototype/` is the **visual source of truth** (exported via the Hallmark skill; `skills-lock.json` pins it): `onclaw-app.html` (single-file React prototype), `DESIGN-MANIFEST.json` (machine-readable screen/token/interaction map), `DESIGN-HANDOFF.md` (the binding contract), `onclaw-preview.png`. The production app must reproduce this design, not reinterpret it. Non-negotiables:

- **Extract tokens before writing components.** The prototype defines CSS custom properties — accent `#2f6feb`, bg `#fafafa`, surface `#ffffff`, border `#e5e5e5`, fg `#111111`, muted `#6b6b6b`; Inter (body/display) + JetBrains Mono; radius 8/12/16/pill; motion 150/200ms `cubic-bezier(0.2, 0, 0, 1)`. Never substitute framework-default themes or typography.
- **Screen-file-first:** each screen is its own route — chat, agents, cron/schedules, runs, workspace settings (members, skills, MCP, API keys), onboarding/workspace creation.
- **Responsive:** must work from 360×800 to 1920×1080 with zero horizontal overflow (full viewport matrix in `DESIGN-HANDOFF.md`).
- **Structured config forms, never raw JSON textareas.** One labeled field per config property — see the prototype's agent config modal (provider select, model, temperature, system prompt, tool toggles, thread retention). Read-only pretty-printed JSON is fine for _display_; textareas only for genuinely unstructured values (system prompts, scripts).
- **Preserve states and patterns:** hover/focus/loading/empty/error/success states, real copy (no marketing filler), monospace chips for models/tools, tool-call cards with latency in transcripts, mention (`@agent`) and slash-command menus in the composer.

## Domain Vocabulary

Use these names consistently across schema, API, and UI (they come from the prototype):

- **Workspace** — the tenant: name, URL slug, timezone. Switchable via the workspace switcher; created through onboarding (optionally with a starter agent).
- **Member** — workspace user with role Owner / Admin / Member.
- **Agent** — persona + config: brief/identity/soul (replaces monolithic system prompt), provider, model, temperature, tools exposed, skills, MCP servers. (Slash commands and thread retention are deferred.)
- **Chat / Thread** — conversation with an agent; transcript includes tool-call cards and cron-origin markers.
- **Channel** — team room (`#ops`, `#incidents`); **Team** — direct messages with members.
- **Cron / Schedule** — name, expression, next/last run, trigger, email routing.
- **Run** — one agent execution: status/state, tokens used, started/last-active.
- **Skills / MCP servers / Tools** — agent-attachable capabilities.
- **API keys / Tokens** — per-workspace credentials.

## Design Contract Deviations
- System prompts are split into `brief`, `identity`, and `soul` instead of a monolithic system prompt.
- Slash commands and thread retention features are marked as deferred.
