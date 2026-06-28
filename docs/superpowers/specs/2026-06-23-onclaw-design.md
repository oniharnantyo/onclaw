# onclaw — Design Spec

- **Status:** Approved (design) — pending implementation plan
- **Date:** 2026-06-23
- **Owner:** oniharnantyo
- **Supersedes:** the "CLI boilerplate" scope described in the current `README.md`

## 1. Overview

**onclaw** is an open, on-device AI agent (Claude Code–style) designed to run on
small single-board computers (~2 GB RAM, 8 GB storage — Raspberry Pi / Orange Pi
class). It ships as a single statically-linked Go binary with no runtime
dependencies.

This spec extends the existing CLI boilerplate into a working agent with three
capabilities on top of the remote-backed agent core:

1. **Understand me** — long-term memory + correction-driven preference learning.
2. **Self-improvement** — the agent can author new skills (plugins) for itself.
3. **Extensibility** — a unified plugin system (prompt-skills + MCP + embedded Lua).

### Goals
- A real agent core (replacing the current `internal/agent` stub) backed by a
  remote LLM, running comfortably within 2 GB RAM.
- Runtime/user configuration stored in a database, mutable without restart
  (add a provider → live next turn).
- A plugin system that is the universal unit of capability — for humans *and*
  for the agent's own self-authoring.
- Memory that makes the agent personalize over time.

### Non-goals (v1)
- Running a local LLM on the device (RAM-infeasible for real coding ability).
- Style/persona adaptation (deferred — largely falls out of preference rules).
- OS keychain secret storage (deferred — unreliable on headless Pi).
- Built-in vector/embedding recall (phase 2, optional, remote embeddings only).

## 2. Key decisions

| Area | Decision |
|---|---|
| LLM brain | Remote API via Eino `ChatModel`; agent loop + tools + memory + plugins run on-device |
| "Understand me" | Long-term memory + correction-learning (style-adaptation deferred) |
| Self-improvement | Memory + preferences + agent-authored skills |
| Plugins | Unified hybrid: prompt-skill (markdown) + MCP server + embedded Lua script |
| Plugin runtime | gopher-lua (Lua) for embedded scripts |
| Process model | Single process (monolith) + pragmatic sandbox |
| Config (bootstrap) | `config.yaml` < `ONCLAW_*` env < flags (Viper) — `db_path`, limits, plugin dirs |
| Config (runtime/user) | **sqlite DB** — provider profiles, API keys, preferences (incl. default provider), active skills |
| Secrets | **Encrypted** (AES-256-GCM) in the `config_secrets` table — never plaintext at rest; `ONCLAW_PROVIDER_<NAME>_API_KEY` env overrides per-process (in-memory only) |
| Secret key source | Hybrid: random `master.key` (0600) by default; optional passphrase (`onclaw unlock`, Argon2id) for strong at-rest. DEK + wrap; switching modes re-wraps the DEK only |
| Hot-reload | fsnotify on `.db` + `.db-wal`, SIGHUP fallback; changes apply on the **next turn** |
| Storage | `modernc.org/sqlite` (pure-Go, preserves `CGO_ENABLED=0`) |
| Recall | In-process BM25 over facts + episodes (no on-device embeddings in v1) |
| Session logs | Per-session append-only `.jsonl` transcripts; indexed by a `sessions` table; resumable |
| `config show` | Redacts every secret (`api_key: ***`) |

## 3. Architecture

Only model inference crosses the device boundary. Everything else — agent loop,
tools, memory, plugins, config — runs locally in one static binary.

```
┌─────────────────────────────────────────────────────────────┐
│                         USER  (terminal)                     │
└──────────────────────────────┬──────────────────────────────┘
                               │ onclaw run/chat/provider/plugins/memory
┌──────────────────────────────▼──────────────────────────────┐
│   CLI  (urfave)   ◀──  flags < env < file < defaults         │
│   streaming output · --provider <name> override              │
└──────────────────────────────┬──────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────┐
│              AGENT CORE   (Eino ADK · single process)        │
│                                                              │
│   Provider ──▶ ChatModelAgent  ◀── Memory recall (BM25)      │
│   registry      (ReAct loop)    ◀── Plugin skills inject     │
│                      │                                       │
│                      ▼  tool calls                           │
│                ┌─────────────┐                               │
│                │ Tool Registry├──▶ builtin   (file / shell)  │
│                │             ├──▶ MCP server (child, lazy)   │
│                └─────────────┘──▶ lua script (sandbox+grants)│
│                                                              │
│   reflect (post-turn) ──▶ Memory  (facts / rules)            │
└──────────────────────────────┬──────────────────────────────┘
                               │  HTTPS · inference only
┌──────────────────────────────▼──────────────────────────────┐
│                 REMOTE LLM   (off-device)                    │
│         Claude · OpenAI · Ollama  (named profiles)           │
└──────────────────────────────────────────────────────────────┘

  ┌─ cross-cutting, read by all, hot-reloaded via fsnotify ─────┐
  │  STORE (sqlite, 0600): llm_providers · config_secrets · prefs │
  │  CONFIG (bootstrap): config.yaml · ONCLAW_* env · flags     │
  │  → changes apply on the NEXT turn, no restart               │
  └─────────────────────────────────────────────────────────────┘
```

### Per-turn data flow

```
   ┌──────────────────── per turn ─────────────────────┐
   │  1. recall     memory   ──▶ inject facts/rules     │
   │  2. skills     plugins  ──▶ inject matched skill   │
   │  3. invoke     ChatModelAgent (stream)             │
   │                   │                                │
   │                   ├── tool call? ─▶ Tool Registry  │
   │                   │◀──── result ───────────────────┘
   │                   ▼                                │
   │  4. stream final answer ──▶ user                  │
   │  5. reflect   ──▶ memory (facts/rules)            │
   └────────────────────────────────────────────────────┘
```

## 4. Module layout (extends existing `internal/`)

```
internal/
  store/        NEW — sqlite connection (modernc.org/sqlite), WAL, migrations,
                       0600 perms; the single shared DB handle
  config/       bootstrap config (Viper) + runtime-config loader (reads
                profiles/prefs from store); hot-reload wiring
  provider/     NEW — provider profile CRUD + build Eino ChatModel from a
                named profile; secret resolution (env > DB)
  agent/        grows from stub: ChatModelAgent wiring, ReAct, streaming loop,
                context budgeting
  plugin/       NEW — manifest, loader, registry, skills, lazy MCP, lua sandbox
  memory/       NEW — facts/rules/episodes/recall(BM25)/reflect
  skillauthor/  NEW (phase 2) — agent-authored plugins
  cli/          extended: run, chat, provider, plugins, memory commands
  logging/      existing
  version/      existing
```

Each package has one responsibility, exposes a narrow interface, and is
unit-testable in isolation. **Runtime-reconfigurability is a first-class rule:**
nothing caches config/profile data into struct fields at startup without a
re-apply path (driven by the hot-reload watcher).

## 5. Agent core

- **Provider build** (`provider/`): builds an Eino `model.ChatModel` from a named
  profile. `provider_type` ∈ `{anthropic, openai, ollama, openai-compatible}`, plus
  `api_base`, `model`. All via **eino-ext**. This is the *only* network touch
  for inference.
- **Agent** (`agent/`): `adk.NewChatModelAgent` with a ReAct config = system
  prompt (base + injected skills + recalled memory) + the tool registry.
  Middleware: `summarization` (stay under `MaxContextTokens: 8192`) always on;
  `plan-task` for multi-step requests; `tool-search` only if tool count grows
  large.
- **Loop** (`agent/loop.go`): streams tokens to terminal, dispatches tool calls
  through the registry, enforces the context budget, honors `Concurrency: 1`.

## 6. Config & secrets (two-tier, DB-backed)

### Bootstrap (file/env/flags, Viper — unchanged model)
`db_path`, `log_level`, `log_format`, `concurrency`, `max_context_tokens`,
`plugin.dirs`, `memory.max_facts`, `memory.session_retention_days`,
`memory.log_deltas`, `plugins.max_mcp_processes`, `plugins.mcp_idle_seconds`.
Required before the DB can be opened.

### Runtime / user (sqlite DB)
Provider profiles, API keys, static preferences (including the **default
provider** selected via `onclaw provider use <name>`), active skills — managed
via CLI CRUD, hot-reloaded.

```yaml
# ~/.config/onclaw/config.yaml  — bootstrap only; NO secrets, NO profiles here
db_path: ""                 # defaults to $XDG_DATA_HOME/onclaw/onclaw.db
# default_provider is a DB preference — set it via `onclaw provider use <name>`
log_level: info
concurrency: 1
max_context_tokens: 8192
plugins: { dirs: [], max_mcp_processes: 2, mcp_idle_seconds: 120 }
memory:  { max_facts: 1000, session_retention_days: 30, log_deltas: false }
```

### Secret resolution (per provider profile)
1. `ONCLAW_PROVIDER_<NAME>_API_KEY` env (per-process override, highest priority)
2. encrypted `config_secrets` row in the DB (file at mode 0600; decrypted in-process via the DEK)
3. none → agent errors with "run `onclaw provider login <name>`"

### Hot-reload
- fsnotify watcher on the DB file **and** `.db-wal` (WAL writes land in the WAL
  before checkpoint; watching both avoids missed events).
- `SIGHUP` fallback: `onclaw provider add|login` writes the DB, then, if a
  long-running onclaw is detected (pidfile/socket), sends `SIGHUP` to force a
  reload.
- Changes apply on the **next agent turn** — an in-flight request finishes with
  the old provider; the next turn uses the new config. No mid-stream model swap.
- Reloadable at runtime: provider profiles (add/edit/remove/switch default),
  plugin set, static preferences, log level. Bootstrap changes (e.g. `db_path`)
  require restart by necessity.

### CLI surface
`onclaw provider [list|use <name>|add|remove|login <name>]`,
`onclaw prefs [get|set <key> <val>]`, `onclaw config show` (redacts all secrets),
`onclaw run --provider <name> "<prompt>"`.

> **Security invariant:** secrets are never stored on the printed `Config`
> struct. `config show` and all log output redact via a dedicated secrets view.
> API keys are **encrypted at rest** (AES-256-GCM); the DB file is mode `0600`.
> Full crypto design (DEK + keyfile/passphrase KEK, threat model) lives in the
> OpenSpec change `add-provider-secrets-storage/design.md`.

## 7. Plugin system

### What a plugin is
A directory with a `plugin.yaml` manifest contributing one or more capability
kinds. Trust maps to isolation:

| Kind | What | Isolation | Trust |
|---|---|---|---|
| prompt-skill | markdown instructions injected into the system prompt when relevant | none (text) | any |
| script tool | sandboxed Lua function exposed as an agent tool | gopher-lua sandbox; FS/net only by explicit grant | user / agent |
| MCP server | child process (or remote URL) exposing MCP tools | OS process isolation | user |
| builtin tool | compiled into onclaw (file/shell/memory) | full trust | core only |

### Manifest example
```yaml
# ~/.local/share/onclaw/plugins/weather/plugin.yaml
name: weather
version: 0.1.0
description: "Current weather for a city"
trust: user

skill: { file: SKILL.md }            # injected when triggered

mcp:
  command: ["mcp-weather-server"]    # OR: url: http://...
  env: {}

scripts:
  - name: get_weather
    description: "Return weather for a city"
    runtime: lua
    file: get_weather.lua
    grants: [net:https://wttr.in]    # explicit; empty = pure compute only
```

### Loader, registry, lifecycle (`plugin/`)
- **Discovery:** scan plugin dirs (config `plugins.dirs` + defaults
  `~/.local/share/onclaw/plugins/`, system dir). Hot-reloaded via fsnotify.
- **Registry** builds the Eino `[]Tool` from builtins + active MCP + scripts.
  Prompt-skills flow into the system prompt instead.
- **MCP lifecycle (RAM-critical):** lazy start (spawn only when a tool from that
  server is first called), idle-kill (terminate after `mcp_idle_seconds`), hard
  cap (`max_mcp_processes`, default 2). Installed plugins cost zero resident RAM
  until used.
- **Scripts:** gopher-lua VM, per-call timeout, output cap, grant allowlist
  enforced (no FS/net symbols exposed unless the manifest grants them).
- **Prompt-skill injection:** small always-on set + on-demand skills (only
  name+description in context; body pulled in by the same BM25 recall engine; a
  `use_skill(name)` tool provides an explicit override) — keeps context bounded.

### Security defaults
- Scripts: no FS/net unless explicitly granted; per-call timeout; capped output;
  manifest validated against an allowlist, unknown fields rejected.
- MCP: minimal env, no inherited secrets beyond what the manifest declares.
- Agent-authored plugins (`trust: agent`, phase 2): written to the writable
  `skills/` dir, logged on first activation, optionally one-time user confirmation.

## 8. Memory & self-improvement (`memory/`, `skillauthor/`)

- **Store:** tables `facts`, `rules`, `episodes`, `corrections` (schema §9).
- **Recall** (`recall.go`): in-process BM25 over facts + episodes; top-K injected
  per turn, bounded to ~512 tokens. Static preferences + active `rules` are
  always in context (rules appended to the system prompt).
- **Reflect** (`reflect.go`): post-turn, best-effort. Two triggers: (1) a detected
  correction → propose a `rule` (optionally confirmed); (2) end-of-turn → extract
  salient facts. Cheap model call or heuristic; skipped when nothing's worth
  keeping. **Failures are logged, never block a turn.**
- **Self-authored skills** (`skillauthor/`, phase 2): a privileged builtin tool
  `write_skill(name, manifest, lua/markdown)` writes a new plugin to
  `~/.local/share/onclaw/skills/`. Closes the self-improvement loop: facts/rules
  = *understand you*; written skills = *grow capability*.
- **Embeddings:** phase 2, optional, remote embedding API only.

### Session transcripts (JSONL)
- Each `onclaw run`/`chat` session is one **append-only** `.jsonl` file under
  `$XDG_DATA_HOME/onclaw/conversations/`, named `<started_at>-<ULID>.jsonl`.
- One JSON object per line per event. Event types include `session_start`,
  `user`, `recall`, `skill`, `assistant`, `tool_call`, `tool_result`,
  `reflect`, `config_reload`, `error`, `session_end`. With `log_deltas: true`,
  streaming `assistant_delta` events are also recorded.
- Append-only and line-buffered; **fsync per turn boundary** (not per line); the
  full session is never held in memory — fits the 2 GB discipline.
- **Privacy:** never log resolved secret values (provider keys); conversation
  content is user data and stays local. Tool args/results are size-capped and
  known-secret patterns are redacted at the logging boundary.
- The `sessions` table (§9) indexes each file for listing/resume; `reflect`
  derives an `episodes` summary from closed sessions for recall.
- **Pruning:** after `memory.session_retention_days` (default 30) raw transcripts
  are summarized into `episodes` and the raw file archived/rotated.
- **Resume:** `onclaw chat --resume <session>` or `--continue` (most recent)
  reloads prior messages into context and appends to the same file.

## 9. Data model (shared sqlite DB)

```sql
-- runtime / user config
CREATE TABLE llm_providers (
  name         TEXT PRIMARY KEY,
  provider_type TEXT NOT NULL,          -- anthropic|openai|ollama|openai-compatible
  api_base     TEXT NOT NULL DEFAULT '',
  model        TEXT NOT NULL,
  settings     TEXT NOT NULL DEFAULT '{}',
  enabled      INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL
);
-- key material and settings
CREATE TABLE preferences (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL
);
-- preferences table holds:
--  - 'wrapped_dek': base64(nonce || encrypted_dek)
--  - 'key_mode': 'keyfile' | 'passphrase'
--  - 'passphrase_salt': base64(salt) (optional KDF salt)
--  - 'default_provider': name of default provider
--  - other preference settings

CREATE TABLE config_secrets (
  key            TEXT PRIMARY KEY,      -- provider profile name
  encrypted_value TEXT NOT NULL         -- base64(salt ‖ nonce ‖ ciphertext ‖ tag), AES-256-GCM
);
CREATE TABLE active_skills (
  plugin     TEXT PRIMARY KEY,
  enabled    INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);

-- sessions (transcript index)
CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,          -- ULID
  started_at TEXT NOT NULL,
  ended_at   TEXT,
  provider   TEXT NOT NULL,
  model      TEXT NOT NULL,
  cwd        TEXT NOT NULL DEFAULT '',
  jsonl_path TEXT NOT NULL,
  tokens_in  INTEGER NOT NULL DEFAULT 0,
  tokens_out INTEGER NOT NULL DEFAULT 0,
  summary    TEXT NOT NULL DEFAULT ''
);

-- memory
CREATE TABLE facts (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,
  content     TEXT NOT NULL,
  source      TEXT NOT NULL,
  confidence  REAL NOT NULL DEFAULT 0.5,
  created_at  TEXT NOT NULL,
  last_used_at TEXT NOT NULL
);
CREATE TABLE rules (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  trigger    TEXT NOT NULL,
  directive  TEXT NOT NULL,
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE TABLE episodes (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  summary    TEXT NOT NULL,
  tags       TEXT NOT NULL DEFAULT '',
  ts         TEXT NOT NULL,
  jsonl_path TEXT NOT NULL
);
CREATE TABLE corrections (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id   INTEGER,
  original  TEXT NOT NULL,
  ts        TEXT NOT NULL
);
```

Episodic raw logs are JSONL files under `$XDG_DATA_HOME/onclaw/conversations/`;
`episodes` holds summaries + an index for recall. Old episodes are summarized and
pruned to bound growth.

## 10. 2 GB memory budgeting

Target resident RAM **≤ ~1.2 GB** so the OS breathes on a 2 GB box.

| Component | Rough resident |
|---|---|
| OS + static binary | 150–300 MB |
| Agent loop + Eino | 50–100 MB |
| sqlite + BM25 index | 30–80 MB |
| 1–2 lazy MCP children | 30–100 MB each |
| gopher-lua VM (active script) | 5–15 MB |

Enforced by: `Concurrency: 1`, `MaxContextTokens: 8192` (both already set),
MCP lazy-start + idle-kill + cap, streaming (no full-response buffering), and
memory pruning (`memory.max_facts`, summarized/pruned episodes). All knobs
configurable.

## 11. Error handling

- Provider: transient (429/5xx) → backoff retry; auth → "run
  `onclaw provider login <name>`"; offline → graceful message.
- MCP child crash → restart once on next call; if repeated, disable plugin + log;
  agent told "tool unavailable."
- Script timeout/panic → tool error to the agent + log.
- Invalid plugin manifest → skip plugin + warn, never crash the agent.
- Memory write failure → best-effort log, never blocks a turn.
- Bad config reload → keep last-known-good, log, do not apply.
- Idiomatic wrapping: `fmt.Errorf("load plugin %s: %w", name, err)`.

## 12. Security and Threat Model

### Cryptographic Boundary and Key Management
The system implements a two-tier key-encryption key (KEK) architecture to wrap the data-encryption key (DEK) used to encrypt all credentials at rest:
1. **Keyfile Mode (Default):**
   - **Mechanism:** A random `master.key` (permission 0600) acts as the KEK source.
   - **Threat Model:** Defeats casual exposure such as database dumps, backup leakage, raw database file search (`grep`), or accidental logging. It does **not** protect against a local disk-access attacker who can read files inside the parent directory (as they can read `master.key` and decrypt the DB). This mode supports unattended restarts.
2. **Passphrase Mode (Opt-in via `onclaw unlock`):**
   - **Mechanism:** KEK is derived from a user-supplied passphrase using Argon2id.
   - **Threat Model:** Protects against full compromise of the storage volume/disk-access attackers. An attacker with the database file but without the passphrase cannot decrypt the credentials. The trade-off is that it requires active human unlocking on startup/restart.

### General Security Invariants
- **Secret Redaction:** Plaintext API keys never appear in log files or on the printed `Config` struct. The structured logger uses a `ReplaceAttr` helper that automatically filters attributes with secret-related keys (e.g. `api_key`) and scans string values for known patterns (e.g. `sk-...` prefixes) replacing them with redaction placeholders.
- **Access Restrictions:** The SQLite database file is created with permission `0600` (owner read/write only). The system fails closed and refuses to operate if it cannot secure the database file or if the file/keyfile has wider permissions.
- **Plugins/Scripts Sandbox:** Scripts run in a gopher-lua sandbox with timed execution limits, output limits, and no filesystem or network access unless explicitly granted in their manifest. MCP child processes run in isolated system processes with a minimal environment.

## 13. Testing (TDD, 80%+ coverage; unit / integration / E2E)

A **stub `ChatModel`** is the keystone — no network in CI and ~zero RAM.
- **Unit:** config layering + redaction; provider build + secret resolution;
  manifest parse/validation; lua grant enforcement; sqlite CRUD; BM25 recall;
  reflect with a stubbed model.
- **Integration:** full turn with fake provider → tool dispatch → memory write;
  hot-reload (write a profile mid-run → assert new provider used next turn);
  plugin install → tool live.
- **E2E:** `onclaw run`/`chat` against a fake/local provider; `onclaw provider
  login` → restart-free availability; plugin install → tool available.

## 14. Phased roadmap (all milestones specced in detail)

### M1 — Agent core + provider profiles + hot-reload + redaction
- `store/`: open sqlite (modernc.org/sqlite), WAL, migrations, 0600; tables
  `llm_providers`, `config_secrets`, `preferences`.
- `provider/`: profile CRUD (CLI) + build `ChatModel` from a named profile
  (eino-ext); secret resolution env > DB.
- `config/`: bootstrap via Viper; runtime-config loader reads from `store`.
- `agent/`: replace stub; `adk.NewChatModelAgent` (ReAct) + summarization
  middleware; `loop.go` streaming invoke honoring Concurrency=1 / 8192 tokens.
- `cli/`: `run "<prompt>"` (streaming), `chat` (interactive), `provider
  [list|use|add|remove|login]`, `config show` (redacted), `--provider` flag.
- Hot-reload: fsnotify on `.db`+`.db-wal` + SIGHUP handler; applies next turn.
- **Exit criteria:** `onclaw provider login claude` then `onclaw run "hi"`
  streams a remote response; a second provider added and selected via
  `--provider` works live without restart.

### M2 — Builtin tools + memory + recall + reflect
- Builtin tools: `read_file`, `write_file`, `list_dir`, `shell` (confirmable),
  optional `web_fetch`. Registered as Eino Tools, full trust.
- `memory/`: tables `facts`, `rules`, `episodes`, `corrections`; `recall.go`
  (BM25 top-K into system prompt); `reflect.go` (correction→rule, end-of-turn→facts).
- Active `rules` appended to the system prompt at assembly time.
- **Session transcripts:** append-only per-session `.jsonl` + `sessions` table;
  `onclaw sessions [list|show <id>]` CLI; `onclaw chat --resume <id>` /
  `--continue` reload prior context.
- `onclaw memory [show|forget <id>|rules]` CLI.
- **Exit criteria:** agent recalls a stated fact across turns; a correction
  becomes a rule that changes subsequent behavior; each session is persisted to
  `.jsonl` and is resumable; `onclaw memory rules` and `onclaw sessions list`
  work.

### M3 — Plugin system (skills + MCP + Lua)
- `plugin/`: manifest parse/validate; loader (scan dirs + fsnotify); registry
  (uniform `[]Tool`); `skill.go` (always-on + on-demand); `mcp.go` (lazy/idle/cap);
  `script.go` (gopher-lua sandbox + grants + timeout + output cap).
- Plugin dirs: user + system + `plugins.dirs`; `skills/` reserved for M4.
- `onclaw plugins [list|enable|disable|install <path>]` CLI.
- **Exit criteria:** drop a `plugin.yaml` with a Lua tool into the plugin dir →
  tool appears live without restart; an MCP plugin launches lazily and is killed
  when idle.

### M4 — Agent-authored skills + hardening
- `skillauthor/`: privileged builtin tool `write_skill(...)` writing to `skills/`;
  activation logged; optional first-run confirmation.
- Optional remote-embedding recall (config-gated).
- Resource-capped plugin host (Approach C-lite): per-plugin memory/time budgets
  for agent-authored plugins.
- Audit logging for self-improvement actions.
- **Exit criteria:** after a repeated procedure, the agent creates a reusable
  skill; the skill is usable; its creation is auditable.

## 15. Deferred / open

- Style/persona adaptation.
- OS keychain secret storage (go-keyring) for laptop use.
- `onclaw config export/import` (DB ↔ YAML) for backup/version control.
- On-device or remote embeddings as the default recall mechanism.
- License (README currently "TBD").
