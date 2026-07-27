# onclaw — High-Level Architecture

High-level block diagram of the major subsystems and how data flows between them.
Drawn from the actual `internal/` package layout and wiring
(`internal/cli/app.go`, `internal/api/server.go`, `internal/agent/agent.go`).

```
┌───────────────────────────────────────────────────────────────────────────┐
│                          CLIENTS / INTERFACES                              │
│   ┌────────────────────────────┐            ┌────────────────────────┐    │
│   │   Web UI  (React + Vite)   │            │      onclaw CLI         │   │
│   │ Chat·Agents·Memory·MCP·    │            │   (urfave/cli v3)       │   │
│   │ Tools·Hooks·Skills·Provs   │            │ run·serve·provider·mcp  │   │
│   └─────────────┬──────────────┘            │ config·unlock·...       │   │
│        HTTP + SSE│                           └─────────────┬──────────┘   │
└──────────────────┼───────────────────────────────────────┼──────────────┘
                   │                                        │
                   ▼                                        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│   ENTRY / WIRING   (internal/cli)   getProviderManager() = app spine       │
│            opens SQLite → loads/decrypts DEK → assembles llm.Service       │
└──────────────────────────────┬────────────────────────────────────────────┘
                               ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                      HTTP API   (internal/api)                              │
│  ┌──────────────┐   ┌────────────────────────┐   ┌──────────────────────┐ │
│  │ server.go    │   │ handler/  (REST + SSE) │   │ service/             │ │
│  │ http.Server  │──▶│ chat·conversation·agent│──▶│ orchestration        │ │
│  │ + static web │   │ memory·mcp·tools·hooks │   │ + auth/ + httpx/     │ │
│  └──────────────┘   │ skill·provider·fs      │   └──────────────────────┘ │
└─────────────────────────────┬─────────────────────────────────────────────┘
                              ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                   CORE DOMAIN  /  AGENT RUNTIME                            │
│  ┌───────────────────────────  Agent  ──────────────────────────────────┐ │
│  │ conversation/  (sessions, chat history, compaction)  ◀── tokens/      │ │
│  │                          (context meter + over-limit guard)           │ │
│  │                                                                       │ │
│  │   ┌────────────┐   ┌──────────────┐   ┌──────────────────────────┐   │ │
│  │   │  llm/      │   │ agent/tools/ │   │ memory/                  │   │ │
│  │   │  Service    │   │ builtin·fs·  │   │ core·episodic·KG·        │   │ │
│  │   │  (facade)  │   │ browser·     │   │ staged-write·embedder·   │   │ │
│  │   │  ▶ adapter/│   │ shellpolicy  │   │ dreamer·pruner           │   │ │
│  │   └─────┬──────┘   └──────┬───────┘   └──────────────────────────┘   │ │
│  │         │                 │  ┌─────────┐  ┌────────┐  ┌───────────┐   │ │
│  │         │                 ├─▶│ mcp/    │  │ skill/ │  │ hooks/    │   │ │
│  │         │                 │  │external │  │ loader │  │ handler   │   │ │
│  │         │                 │  │ tools   │  └────────┘  └───────────┘   │ │
│  │         │                 │  └─────────┘                              │ │
│  └─────────┼─────────────────┴──────────────────────────────────────────┘ │
│            ▼ eino ChatModel                                                │
└────────────┼──────────────────────────────────────────────────────────────┘
             │
   ┌─────────┴──────────┐   ┌─────────────────────────────────────────────┐
   │ secrets/           │   │ config/ (Viper): defaults < config file <   │
   │ DEK/KEK, AES-256-  │◀━━│ ONCLAW_* env < CLI flags                    │
   │ GCM; decrypts keys │   │ hot-reload via PID file + SIGHUP            │
   └─────────┬──────────┘   └─────────────────────────────────────────────┘
             │ encrypts secrets at rest
             ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                       PERSISTENCE   (store)                                │
│   store/  (interfaces + DTOs)   ──implemented by──▶   store/sqlite/        │
│   profiles·secrets·kv·conversations·memory·agents·mcp·hooks·skills         │
│   ┌─────────────────────────────────────────────────────────────────────┐ │
│   │   Pure-Go SQLite  (CGO_ENABLED=0 → cross-compiles to ARM)            │ │
│   └─────────────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────────────┘

Cross-cutting (used everywhere):  logging/(redaction) · observability/ ·
                                  modelmeta/ · render/ · workspace/ · version/
Web/tool backends:  web/(google·ddg·exa·tavily·http·lightpanda) · browser/cdp
```

## How to read it

Request flows **top → bottom**. A browser screen or shell command enters via
`cli`, hits the HTTP API, which calls into the Agent runtime. The Agent
orchestrates the LLM, tools, and memory over a conversation; everything
reads/writes through `store/sqlite` at the bottom; secrets are decrypted
in-memory by `llm` and never persisted in the clear.

## Why it's shaped this way

- **Two entry points, one spine.** Both the React web UI and the `onclaw` CLI
  converge on the same `cli` → `api` → core path. `getProviderManager()` in
  `internal/cli/context.go` is the single assembly point — it opens SQLite,
  unwraps the DEK, and builds the `llm.Service`, so every subsystem starts from
  an already-decrypted, DB-backed state.
- **Contract / types / impl split.** `store/` holds only interfaces + DTOs while
  `store/sqlite/` is the lone implementation — that's why the whole app runs on
  pure-Go SQLite (no CGO/libc) and cross-compiles to ARM for the Pi-class
  target. The same pattern appears in `llm/` (facade) vs `llm/adapter/`
  (swappable providers; only a stub today).
- **`secrets/` is a side-band, not a layer.** It wraps *specific values* — API
  keys for `llm`, encrypted secret rows for `store`. The DEK/KEK split lets you
  re-key (`onclaw unlock`) without touching stored data, and `logging/` redacts
  known credential fields so secrets never leak upward through the layers.
