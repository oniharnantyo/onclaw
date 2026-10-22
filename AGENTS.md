# AGENTS.md

Minimal pointer file. The rules are the source of truth and live in [`.agents/rules/`](.agents/rules/) — read **every** file there before making any change. This file imports them by reference and intentionally does not duplicate them.

@.agents/rules/01-project-overview.md
@.agents/rules/02-architecture.md
@.agents/rules/03-workflow.md
@.agents/rules/04-testing.md
@.agents/rules/05-coding-standards.md
@.agents/rules/06-commit-protocol.md
@.agents/rules/07-documentation.md
@.agents/rules/08-testing-standards.md
@.agents/rules/karpathy-guidelines.md

## Project

**buddies** — a Wails3 desktop app (Go backend + embedded webview frontend), rebranded from `onclaw-v2`.
Direction: a local gateway that keeps running in the background (system tray) even when the window is closed, accepting triggers from cron, webhooks, Telegram chat, and more.

## Critical lines

- Commands are **Task** (`task`), not make. Dev: `task dev` (hot reload). Build/run: `task build` / `task run`. Server mode (no GUI): `task build:server` / `task run:server`.
- Before finishing any Go change: `gofmt -w .`, `go vet ./...`, `go test ./...`.
- Feature work goes through the OpenSpec workflow (see `openspec/` and the `openspec-*` skills), not ad-hoc changes.
- The imported rules originate from a Go-service template; where they mention make targets or a hexagonal `internal/` layout that does not exist here, this repo's Task commands and Wails3 layout win.
