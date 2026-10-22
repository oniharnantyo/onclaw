---
description: "How to make, verify, and finish a change"
---
# Rule: Workflow (how to make a change)

## Before starting
- Read `AGENTS.md`, `CLAUDE.md` (if present), `rules/` (this directory), and `docs/architecture.md`.
- Locate the scaffolded layers and the health handler (`internal/adapters/http/health/`) as the reference shape.

## While working
- One task = one vertical slice = one change. Unrelated edits belong in a separate change.
- Never invent APIs, config keys, or provider names. If you cannot verify something, ask the human.
- Never edit derived/generated files directly: `.claude/rules/`, `.cursor/rules/` - edit their source (`rules/`) and run `make rules`.
- If a step conflicts with an existing test or rule, stop and surface the conflict - do not silently weaken the rule.

## Before finishing
1. `make fmt` (or gofmt/goimports) and `go vet ./...`
2. `make test` (runs with `-race`)
3. `make lint` (golangci-lint, strict config)
4. `make arch` (go-arch-lint architecture check)
5. Update docs per 07-documentation.md
6. Close with the handover summary per 08-agent-handover.md

Success criteria: everything above is green, no rule weakened to get there.
