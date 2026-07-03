# AGENTS.md

Minimal context for agents working on onclaw. Full details: see `CLAUDE.md`.

## Non-obvious constraints

**Build requirements** (agents miss this):
- `CGO_ENABLED=0` is **mandatory** — pure-Go SQLite (modernc.org/sqlite), no libc dependency needed for cross-compilation to ARM
- Memory footprint matters: defaults tuned for ~2GB devices (conservative `max_context_tokens: 8192`, `concurrency: 1`)

## Architecture

**Assembly root**: `internal/cli/context.go` `getProviderManager()` — opens SQLite DB, manages DEK/KEK encryption, assembles `llm.Service`. Read this first.

**Hot-reload mechanism**: Provider profile edits write PID file and `SIGHUP` the running process; `fsnotify` watcher is fallback. Both set `Service.reloadPending` flag.

## Web UI

Frontend lives in `web/`. Follow the design system at `web/design-system/onclaw/MASTER.md` (it owns colors, typography, components, and anti-patterns for all UI work).

**Config forms must be structured fields, never a raw JSON input.** Every configuration dialog renders one field per property (select / text / number / checkbox, each with a label, tooltip, and inline validation), derived from that config's JSON schema or DTO — never a single editable JSON `<textarea>`. A read-only JSON `<pre>` preview is allowed for *displaying* stored config, never for editing it. Free-text/code values whose content is genuinely unstructured (hook scripts, agent system prompts) stay as textareas — that is a value, not structured config. Current offenders to fix: Browser category config (`web/src/components/Tools.tsx`) and the MCP server `env` field (`web/src/components/MCP.tsx`). Full rationale in `CLAUDE.md` → Web UI.

## Code conventions

**Store package structure** (STRICT separation):
- `types.go` — DTOs only
- `store.go` — interfaces only (no implementations)
- `sqlite/*.go` — concrete implementations (one file per entity)
- Never co-locate interface with implementation

**Error handling**:
- Use `fmt.Errorf` with `%w` for wrapping (required for `errors.Is`/`errors.As`)
- `context.Context` must be first parameter for all public functions
- Return early on errors — success path runs down the page

**Feature planning**: Check `openspec/changes/` before implementing non-trivial features.

## Quick reference

```bash
make build       # static, stripped binary
make test        # all tests
go test ./path/...  # focused testing
make lint        # golangci-lint, falls back to go vet
```

## Status note

`onclaw run` is a placeholder. `internal/agent/` is stub. Only a stub LLM adapter is registered.