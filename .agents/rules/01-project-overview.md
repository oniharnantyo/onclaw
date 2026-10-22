---
description: "Project identity, stack, and what AI-ready means in this repo"
---
# Rule: Project Overview

## What this project is
- **Ixion** — the **Marketing PCB Tools backend**: a Go service using **hexagonal architecture** (ports & adapters).
- The repository is **AI-ready**: built to be developed *with* AI agents (Claude Code, Cursor, Codex, Antigravity, Kiro, ...).
- It is **NOT** an AI/LLM application. Keep it that way: no AI plumbing unless explicitly requested by the human.
- Today it ships only the **health surface** (`/healthz`, `/readyz`) plus the shared HTTP plumbing; the hexagonal layers are scaffolded for the first feature slice.

## Stack (change only with the human's explicit approval)
- Go 1.26+
- **urfave/cli/v3** (`urfave/cli/v3`) for CLI commands and subcommands
- **Echo v4** (`labstack/echo`) for the HTTP driver adapter
- `log/slog` for structured logging
- `jackc/pgx/v5` for PostgreSQL (repository adapter + readiness ping; optional)
- `gopkg.in/yaml.v3` for YAML configuration (`config.yaml`)
- `golangci-lint` + `go-arch-lint` for machine-checked code quality and architecture
- Minimal dependencies on purpose (see 05-coding-standards.md)

## Layout at a glance
| Path | Responsibility |
|---|---|
| `main.go` | entry point, CLI commands (urfave/cli/v3), manual dependency wiring (DI) |
| `config/` | configuration loader (`config.yaml` + env overrides) |
| `internal/domain/` | business entities - one file per aggregate - pure, zero deps |
| `internal/application/ports/driver/usecases/` | **driver ports**: capabilities the outside world may call (usecases) |
| `internal/application/ports/driven/repositories/` | **driven ports**: what the core needs from the outside world (repositories) |
| `internal/application/usecases/` | use case implementations grouped per domain (scaffolded, empty until the first slice) |
| `internal/adapters/http/` | **HTTP adapter**: REST API endpoints, middleware, error mapping (Echo) |
| `internal/adapters/repositories/` | **storage repositories**: implementations of persistence driven ports (postgres) |
| `pkg/` | genuinely small shared utilities (logger, id) |

## The canonical pattern
Every new feature is a **vertical slice**: domain -> driver/driven ports -> use cases -> adapters -> tests, following the checklist in `docs/architecture.md`. The layers are scaffolded but no example slice ships yet — `internal/adapters/http/health/` is the reference for the HTTP shape. When a slice lands, extend `docs/architecture.md` so the next agent can copy *that* slice.
