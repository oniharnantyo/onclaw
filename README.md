# OnClaw

OnClaw is a multi-tenant, self-hosted **AI agent workspace** — an OpenClaw / Hermes alternative. Each tenant gets a **workspace** where members create named **agents** configured with a system prompt, provider/model, temperature, exposed tools, skills, slash commands, and MCP servers. Agents are reached through direct chats, team **channels** (`#ops`, `#incidents`), and **cron schedules** (e.g. a morning digest); executions appear as **runs** with tool-call cards in the transcript.

## Architecture

OnClaw ships as a single Go binary exposing a REST API under `/api/v1`, backed by PostgreSQL. The web app is a separate Vite + React + Tailwind SPA that talks to the API same-origin (in dev, the Vite dev server proxies `/api` to the backend).

```
main.go             Root entrypoint: loads .env, builds the root command via internal/cli, runs
internal/
  cli/              Composition root: commands (server, migrate, user, superadmin) & driver registration
  config/           Configuration from flags, environment variables, and .env
  domain/           Entities, validation rules, permission catalog & algebra
  auth/             Auth ports & registry, password hashing (argon2id), JWT (HS256)
  server/           HTTP router, middleware, REST API handlers under /api/v1
  store/            Data storage ports & in-memory test fake
  store/postgres/   PostgreSQL store adapter (pgx/v5) & embedded migrations runner
  storage/          File/blob storage port
  storage/local/    Local filesystem storage driver with atomic writes & capability URLs
  bootstrap/        Fresh-instance lifecycle: master tenant + initial superadmin seeding
migrations/         Numbered up/down SQL migration pairs
scripts/            smoke.sh end-to-end curl suite, e2e-verification.sh
web/                Vite + React + Tailwind SPA (see web/README.md)
```

## Prerequisites

- **Go 1.27**
- **PostgreSQL 14+**
- **Node 20+** and **pnpm 10** (`corepack enable`) for the web app

## Quickstart

```bash
# 1. Install backend dependencies
go mod download

# 2. Create the database
createdb onclaw   # or: psql -c 'CREATE DATABASE onclaw;'

# 3. Configure via .env
cp .env.example .env
# edit .env: set DATABASE_URL (required), ONCLAW_ENCRYPTION_KEY (required for server: openssl rand -hex 32), and ONCLAW_JWT_SECRET (stable sessions)

# 4. Apply migrations
go run . migrate up

# 5. Start the API server (listens on :8080)
go run . server

# 6. Create the initial superadmin (from a second terminal)
go run . superadmin create --email you@example.com --name You --password '...' \
  --database-url "$(grep '^DATABASE_URL=' .env | cut -d= -f2-)"

# 7. In another terminal, start the web app
cd web
pnpm install
cp .env.example .env
pnpm dev          # http://localhost:5173 — /api is proxied to :8080
```

Log in at `http://localhost:5173` with the superadmin credentials. Alternatively, set `ONCLAW_SUPERADMIN_EMAIL`/`ONCLAW_SUPERADMIN_PASSWORD` in `.env` and the server seeds the superadmin automatically on startup.

## Configuration

Configuration comes from four sources, in order of precedence:

1. **CLI flags** (`--listen-addr`, `--database-url`, …) — highest
2. **Real environment variables**
3. **`.env` file** (working directory) — loaded automatically by every command; never overrides an already-set variable
4. **Built-in defaults** — lowest

Notes:
- A malformed `.env` aborts the command with a loud error.
- An empty value in `.env` (`VAR=`) is applied rather than ignored; optional settings then fall back to their built-in defaults downstream, while required ones (`DATABASE_URL`) fail with a clear error.
- **Tests do not read `.env`** — integration tests read `TEST_DATABASE_URL`/`DATABASE_URL` from the real environment.

### Backend variables

| Variable | Default | Flag | Description |
|---|---|---|---|
| `DATABASE_URL` | — | `--database-url` | PostgreSQL DSN. **Required** for `server`, `migrate`, `user`, `superadmin`. |
| `ONCLAW_ENCRYPTION_KEY` | — | `--encryption-key` | Instance master encryption key (32-byte hex or base64). **Required** for `server` (generate with `openssl rand -hex 32`). Key rotation requires re-entering provider keys. |
| `ONCLAW_LISTEN_ADDR` | `:8080` | `--listen-addr` | HTTP listen address. |
| `ONCLAW_JWT_SECRET` | ephemeral | `--jwt-secret` | JWT signing secret (HS256). Unset → ephemeral secret, sessions invalidated on restart. |
| `ONCLAW_TOKEN_TTL` | `24h` | `--token-ttl` | Access token lifetime (Go duration string). |
| `ONCLAW_DATA_DIR` | `./data` | `--data-dir` | Data directory for the local storage driver. |
| `ONCLAW_STORAGE_DRIVER` | `local` | `--storage-driver` | Storage driver registry name. |
| `ONCLAW_SUPERADMIN_EMAIL` | — | `--superadmin-email` | Initial superadmin email, seeded at startup if missing. |
| `ONCLAW_SUPERADMIN_PASSWORD` | — | `--superadmin-password` | Initial superadmin password. |
| `ONCLAW_SUPERADMIN_PASSWORD_FILE` | — | `--superadmin-password-file` | Read when the password variable is empty. |
| `TEST_DATABASE_URL` | falls back to `DATABASE_URL` | — | PostgreSQL DSN for integration tests (`go test -tags=integration`). Real env only — not read from `.env`. |

### Web variables (Vite)

| Variable | Default | Description |
|---|---|---|
| `VITE_API_URL` | *(unset)* | Absolute API origin baked into the client bundle at build time. Unset → same-origin `/api/v1`. Cross-origin values require CORS or a reverse proxy — **the backend currently sends no CORS headers**. |
| `VITE_API_PROXY_TARGET` | `http://localhost:8080` | Dev-server only: proxy target for `/api` requests. Not used in production builds. |

Frontend values are read when the dev server or build starts; restart after changing.

## CLI reference

```bash
onclaw server                                                          # run the API server
onclaw migrate up                                                      # apply all pending migrations
onclaw migrate down --steps N [--all]                                  # rollback migrations
onclaw migrate status                                                  # current migration state
onclaw migrate version                                                 # print migration version
onclaw user create --email <e> --name <n> --password <p> [--avatar-file <path>]
onclaw user list
onclaw user disable --email <e>
onclaw superadmin create --email <e> --name <n> --password <p>
```

## Testing

```bash
# Backend
go build ./...
go vet ./...
go test ./...                                  # unit & fake-based tests (no DB needed)
go test -tags=integration ./...                # requires PostgreSQL: TEST_DATABASE_URL
./scripts/smoke.sh                             # end-to-end curl suite (spins up its own server on :8088)

# Web (from web/)
pnpm build         # typecheck (tsc -b) + production build
pnpm lint          # oxlint
pnpm test          # vitest unit tests
pnpm test:e2e      # Playwright tests
```

## Troubleshooting

- **`database URL is required`** — `DATABASE_URL` is neither set in the environment, `.env` (in the current working directory), nor via `--database-url`.
- **`ONCLAW_ENCRYPTION_KEY is required`** — the server refuses to start without a valid 32-byte encryption key. Generate one with `openssl rand -hex 32` and set `ONCLAW_ENCRYPTION_KEY` in `.env` or pass `--encryption-key`. Key rotation requires re-entering stored provider keys.
- **`jwt secret is not configured` warning** — the server uses an ephemeral secret; users are logged out whenever the server restarts. Set `ONCLAW_JWT_SECRET` in `.env` for stable sessions.
- **Address already in use** — another process holds the port; change `ONCLAW_LISTEN_ADDR` or stop the other process.
- **`.env` changes are not picked up** — `.env` is read from the **current working directory** at process start; restart the command (and the Vite dev server for frontend vars).
