## Context

The runtime already has every seam hooks need: the run entry (`Runner.run` → `streamRun`), the eino middleware chain (`buildMiddlewares`, which wraps tool execution through both `WrapInvokableToolCall` and `WrapStreamableToolCall` — the ToolsNode executes via the streamable chain), and the terminal handling that mints `turn_completed` / `error` / `cancelled` transcript events. The approval flow (`adk.InterruptSignal` → `Resume`) is the *human* gate; hooks are the *machine* policy gate. Precedents for scoped capability config exist: MCP servers (two-table registry + opt-in, encrypted env/headers), web search stacks (inline encrypted envelopes inside one JSONB config), skills (three-tier governance by rule, per-agent toggles deliberately deleted), and the bootstrap seeding pattern (`EnsureMaster`/`SeedSuperadmin`, builtin-role permission backfills). The webfetch tool already enforces SSRF guards (loopback/private/link-local blocking with redirect checks). See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**
- Lifecycle hooks that observe or block agent runs at five events, with one uniform decision contract across four handler types.
- Policy semantics: hooks that apply to a scope apply to *all* of it (workspace hooks reach every agent; instance hooks are irrevocable from below).
- Safe multi-instance operation with a release-driven builtin update story (system-skills equivalence).
- Full audit and health surfacing so a silently broken hook is impossible to miss.

**Non-Goals:**
- Arg modification (`updatedInput`), context injection (`additionalContext`), CEL expression matchers, arg-level matching.
- LLM-evaluator decision caching, monthly token budgets, circuit breaker, chain budget.
- In-process/script-plugin handler code (OpenClaw's model — wrong trust level for multi-tenant Go).
- Subagent, channel-delivery, and gateway lifecycle events (features OnClaw does not have).
- Windows support for command hooks.

## Decisions

**D1 — Event set: five events, two blocking.** `run_started` (observe), `user_prompt_submit` (blocking), `pre_tool_use` (blocking), `post_tool_use` (observe), `run_finished` (observe). `run_finished` fires on **all** terminal outcomes with a `status` field (`completed|failed|cancelled`) rather than three separate events — failure alerting is the top observer use case, and status-as-data absorbs the dropped events. Every event payload carries `origin` (`user|cron|channel`) so cron runs are gated too. Alternatives considered: GoClaw's seven events (rejected: subagent events have no runtime); separate `run_failed`/`run_cancelled` events (rejected: combinatorial growth for what is one datum).

**D2 — Two blocking seams, one dispatcher.** A single `HookDispatcher` (new `internal/agents/hooks` package, wired via a `WithHooks` functional option) with three call sites: run entry (before `composeAgent` — `user_prompt_submit`, `run_started`), a new `hooksMiddleware` appended in `buildMiddlewares` (`pre_tool_use`, `post_tool_use`), and terminal handling (`run_finished`). The middleware wraps BOTH endpoint flavors — the known eino gotcha; the ToolsNode executes through the streamable chain, so wrapping only the invokable chain never sees the call. Resolve once per run (workspace + instance + agent definitions, compiled matchers, list order), not per event.

**D3 — Block = tool-result JSON, never a run failure, never an interrupt.** A blocked `pre_tool_use` returns `{"blocked_by_hook": ..., "reason": ...}` as the tool result the model reads and adapts to (the established `toolErrorResult` shape); the run survives, the transcript shows a blocked tool card, an audit row lands. The interrupt lane stays exclusively the human approval flow. Gate ordering: the hooks middleware sits OUTSIDE the approval interrupt — machine policy evaluates first; a blocked call never pages a human.

**D4 — Call-ID dedup across approval resume.** An approved shell command re-executes through the middleware chain on `Resume`, which would fire `pre_tool_use` twice for one logical call. The middleware caches the decision per `CallID` for the run's lifetime (GoClaw's decision-cache / audit-dedup analog).

**D5 — Observers are harmless by construction.** `run_started`, `post_tool_use`, `run_finished` execute on a detached context (`context.WithoutCancel` + own timeout + panic recovery) so run teardown never kills an in-flight delivery. `post_tool_use` taps stream frames in passing rather than draining (drain would tax every tool call; keeps-up-consumer constraint as in live-reattach). `run_finished` fires after the terminal event settles (cancel path: after `drainToEOF`).

**D6 — A blocked prompt terminates well.** `user_prompt_submit` block = notice transcript event + well-formed `turn_completed` terminal + persisted history entry, model never called, zero tokens. Prevents both the loading-hang bug class and the ghost-history class (reloaded thread shows why the turn is empty).

**D7 — Multi-instance rule: skip what you cannot interpret.** Hooks are read per run (never cached at boot), so content converges the moment the first upgraded pod syncs. The one hazard is capability skew — an old binary loading a builtin row it can't understand. The resolver MUST gracefully skip a hook with an unknown handler type, unknown event, or uncompilable matcher: warning, hook `status` → `error`, continue per `on_failure`. Never fail the run over a definition the binary is too old to execute.

**D8 — Matcher: tagged union, event-aware, validated with a match count. (SUPERSEDED post-implementation → see D19; kept for the decision trail.)** `{"type": "tools", "mode": "all|except|only", "tools": [...]}` or `{"type": "regex", "pattern": "..."}`, stored as one `matcher` JSONB column that IS the UI state (the type tag replaces provenance inference — regex is never parsed back into chips; CC's char-set inference rejected because of its own documented hazard table). Tools entries are exact names or trailing-`.*` prefix globs (browser family = `browser.*`, MCP server = `mcp__<server>__*`). Regex = RE2 (linear-time, no ReDoS), unanchored (power-user semantics; the picker covers full-match needs), 256-char cap, compiled once per run. The matcher value is event-aware: tool name on tool events, `origin` on run-start/prompt events, `status` on `run_finished` — one matcher shape serves every event ("notify only on failures" is literally `{"type":"tools","mode":"only","tools":["failed"]}`). Save validation compiles and reports `{"matched": N, "of": M}` against the workspace-visible toolset (registry + browser expansion + workspace MCP tools; fixed enums for origin/status) — the guard that makes both silent-no-match and silent-over-match visible at authoring time. A non-matching hook is skipped before any dispatch: no HTTP, no audit row, no latency.

**D9 — Handler registry, one contract.** `Handler interface { Execute(ctx, Event, budget) (Decision, error) }`; `http`, `command`, `mcp_tool`, `prompt` register as built-ins (plugins-first: future types are registrations, not edits). Every type may emit the same decision JSON; success without one = allow; any failure (timeout, non-zero exit, MCP error, evaluator silence) → the shared `on_failure` policy (per hook, `allow` default; `block` for deliberate safety gates). Named after the failure policy rename: it covers timeout AND error, unlike a pure `on_timeout`. Doctrine (from Claude Code's own docs): hooks are automation and soft gates — the tool gate and approval flow remain the security boundary; hence fail-open default and the kill-switch below.

**D10 — Command handler: exec form, stdin JSON, exit-2 compatible.** Config mirrors MCP stdio exactly: `command` + `args` array (no shell strings — the injection class does not exist), secret `env` rows, optional absolute `cwd` (default server cwd; never the agent jail). Event JSON on stdin, then EOF-close; payload strings truncated at 64 KB. Strict stdout rule: the ENTIRE trimmed stdout must parse as a JSON object to count as a decision, else ignored (logs go to stderr; lenient find-the-JSON parsing is a decision-injection channel). Decision table: exit 0 = allow; exit 0 + stdout decision JSON = honored; stdout JSON + exit 2 → exit 2 WINS (Claude Code compatibility: existing ecosystem scripts block unchanged); exit 2 + stderr = block with truncated(256) stderr as reason; exit 2 + empty stderr = `"blocked by hook <name>"`; any other exit, signal kill, spawn failure, or timeout = failure → `on_failure` (spawn failure also flips hook `status` → `error`). Environment is THE red line: the server env holds `ONCLAW_JWT_SECRET` and `DATABASE_URL`, so the child gets `PATH`/`LANG`/`TMPDIR` + the hook's own env rows only (deny-by-default, stricter than Claude Code's inherit-all). Teardown: `exec.CommandContext` with a custom Cancel sending SIGTERM and `WaitDelay: 2s` forcing SIGKILL; stdout/stderr drained concurrently (the 64 KB pipe deadlock). Run-cancel mid-hook propagates via the run context (child dies; the run is tearing down anyway) while observer-command hooks on detached contexts survive. `exec.LookPath` at save = warning, not error. Operator kill-switch `ONCLAW_HOOKS_COMMAND_ENABLED` (default on — consistent with stdio MCP shipping enabled). No retries, no queueing.

**D11 — mcp_tool handler: workspace servers, closed placeholders.** References workspace-level MCP servers only (validated at save; agent-private servers unreachable from a workspace hook). Input is structured key/value rows whose values may contain a closed placeholder set: `${event.tool.name}`, `${event.tool.args.<field>}`, `${event.agent.name}`, `${event.origin}` — string substitution, not a template engine. Decision JSON parsed from the tool's text result; else success = allow, MCP error = `on_failure`. Cold MCP connection can exceed a 5 s budget on first fire (cached by MCPManager thereafter).

**D12 — prompt handler: sandboxed evaluator.** The evaluator sees tool name + tool args ONLY (never the user message, session history, or tool results); args are embedded as clearly delimited untrusted data. The evaluator runs as a tool-forced agent whose only tool is `decide(decision, reason, injection_detected)`; no `decide` call = evaluator failure → `on_failure` (free text is never read — that is the injection channel); `injection_detected: true` → block. Config: policy prompt template, EXPLICIT provider + model resolved through the workspace tenant-provider catalog (a narrow evaluator-model-factory injected into the dispatcher — no silent default model), `max_invocations_per_run` (default 5; over cap → allow + audit row), default timeout 15 s at save. A matcher is REQUIRED for prompt hooks — a match-all LLM call on every tool call is a cost incident (GoClaw's same rule). Audit rows gain a nullable `token_count`.

**D13 — Three-level scope, skills-style governance.** Instance hooks are MANDATORY (workspaces see them, cannot disable or weaken; only the superadmin deletes). Workspace hooks are ALWAYS-ON for every agent in the workspace (per-hook enabled switch) — the per-agent opt-in (`enabled_hooks`) model is deliberately NOT built; skills deleted its per-agent toggles as inert, and policy that half the agents opted out of is not policy. Agent hooks are private definitions owned by one agent, auto-attached (the `agent_mcp_servers` shape). The agent config modal shows read-only visibility of instance/workspace hooks plus CRUD for agent-level hooks.

**D14 — Ordering is the list; the tier order is fixed.** No numeric priority (GoClaw is the ecosystem outlier; Claude Code and OpenClaw use declaration/registration order). Execution order within a level = the settings list order (`position` column, drag to reorder, API create order for REST users). Across levels: instance → workspace → agent. First block wins; lower tiers add blocks, never remove them; cheap-before-expensive is achieved by dragging (script gate above the LLM evaluator).

**D15 — Instance tier: embedded definitions, materialized rows (Shape B).** Builtin hooks are defined in the repo (`internal/agents/hooks/builtin/`: Go slice with stable `key` + `version` int). `SyncBuiltinHooks` runs in `internal/bootstrap` every startup: upsert `ON CONFLICT (source, key) DO UPDATE ... WHERE instance_hooks.version < EXCLUDED.version` (idempotent, race-safe, never downgrades); a builtin removed upstream deletes its row, with `hook_executions` preserved via `ON DELETE SET NULL` + denormalized hook name. Rows carry `source = builtin|managed`: builtin rows are read-only to everyone and re-synced on every start; managed rows are superadmin CRUD with the same mandatory semantics. Update story = system skills: edit in repo → build → restart → live; content changes never need migrations. v1 ships zero builtins — the pipeline exists so a security release can add one. `instance_hooks` is the one deliberately workspace-unscoped table (commented as such in the migration; exposed only through the instance-admin permission surface).

**D16 — Storage.** One migration: `instance_hooks`, `workspace_hooks`, `agent_hooks` (each: event, matcher JSONB, `handler_type`, `config` JSONB, timeout, on_failure, enabled, position; the agent table carries `agent_id` FK cascade; instance rows carry `source` + `version` + `key`), plus `hook_executions` (hook id SET NULL on delete + denormalized name, event, decision, duration, exit code/HTTP status, error/stderr snippet truncated to 256, nullable `token_count`). Secrets (http headers, command env) live as inline AES-GCM envelopes inside `config` (web-search-stacks precedent), AAD = workspace ID, keep-stored merge on update. `agents` gains no hook columns. `status` column per hook (MCP pattern) updated per delivery.

**D17 — Permissions and API.** New catalog permissions `hooks.read` / `hooks.write` (+ backfill migration granting them to built-in Superadmin/Owner/Admin roles — roles snapshot permissions at creation). Workspace hooks: REST under workspace scope (handlers pattern of mcp.go/tools.go). Instance hooks: superadmin CRUD for managed rows + read-only builtin listing under the instance-admin surface. Hook resolution at runtime reads all three levels server-side; no new auth surface in `/v1`.

**D18 — Observability.** Hook executions mint transcript events at their seam (new transcript event kind(s) alongside the existing catalog) so blocks are visible where the user reads; the same records feed the settings execution-history view. The Test button performs a REAL execution (spawn/POST/evaluator call) of a synthetic event, shows decision badge / duration / exit code (command) / HTTP status / token count, and writes no audit row.

**D19 — Matcher simplified to Claude Code's single string (post-implementation amendment, supersedes D8).** One `matcher` string replaces the tagged union (user decision: "simplify the matcher, follow Claude Code"). Reading — CC's tiered interpretation adapted for dotted tool names: empty or `*` selects every occurrence; otherwise split on `,` `|` whitespace, and if EVERY entry fits the tool-entry charset (`[A-Za-z0-9_.-]+` with optional trailing `.*`) the entries match exactly or by family (the adaptation: CC's charset rule would push `web.search` into the regex tier); any other character puts the WHOLE string into unanchored-RE2-regex tier (≤ 256 chars, compile-checked at save). The event-aware value is unchanged (tool name | origin | status). The match-count report survives as the hazard control — live in the dialog while typing, and in the save response. `except` is DROPPED: RE2 has no negative lookahead, so "all except" has no single-string form (closed sets like origins/statuses invert trivially; open tool sets rewrite as allow-lists). Storage: `matcher` JSONB → text on all three tables (migration 000029, one-time dev-data conversion: `only` → `a|b`, `except` → match-all, regex → pattern; nothing committed/prod exists). API shape: `"matcher": "shell|read_file"`. UI: the Structured|Regex segmented control, mode dropdown, and chips are replaced by one text input with the live count and a static syntax helper; the list rows carry the plain string.

**D20 — `if`: Claude Code's input-level gate.** An optional per-hook `if` condition on `pre_tool_use`/`post_tool_use` only, in CC's rule form `ToolName(pattern)`: the name part follows the matcher entry rules (exact name or trailing-`.*` family), the pattern is an unanchored RE2 regex (≤ 256) matched against the serialized tool input JSON. A non-matching call SKIPS the hook — no execution, no audit row; `if` never blocks by itself (it narrows, the matcher selects). Malformed or un-compilable at save → 422 field error; uninterpretable at runtime → D7 graceful skip. Splitting at the FIRST `(` with a required `)` suffix, so patterns may contain parentheses. Stored as an `if_rule` column on all three hook tables (000028 — named for the SQL keyword); API JSON key `if`. UI: one optional field between Matcher and Handler, visible only on tool events, placeholder `read_file(secret*)`.

**D21 — Vocabulary aligned to Claude Code; divergences deliberate.** Config keys renamed for CC portability: mcp_tool `server_id` → `server`, `tool_name` → `tool`; prompt `prompt_template` → `prompt` (the explicit provider+model is RETAINED — CC's single `model` would reintroduce the silent-default-model billing hazard D12 rejects). Kept deliberately different: `timeout_ms` (ms precision; seconds-only would cost a migration for cosmetics), header/env secrets as sealed keep-stored rows instead of CC's `$VAR` interpolation (multi-tenant vs local single-user), snake_case event names (DB/API vocabulary — CC's CamelCase is their settings-file convention), and no `async`/`statusMessage`/`once` (our observers are always detached; the prompt cap covers `once`).

**D22 — Script handler: in-process Goja, zero-I/O sandbox (post-implementation amendment).** A fifth handler type `script` (user decision: "use goja") runs author-written JavaScript inside the server process via `github.com/dop251/goja`, a pure-Go ES6+ interpreter — no cgo, no per-platform builds, so the single-binary self-hosted deploy survives intact (v8go rejected: cgo + statically-linked V8 per OS/arch, and a native panic takes the whole server down). Persistence is free: `config.script` is one string inside the existing encrypted JSONB — no interpreter field, no env rows, no cwd, no managed files, no migration. Contract: `config.script` holds the COMPLETE handler function `(function(input){ … })` — an invoked function expression the editor displays in full; the runtime compiles the stored source as-is, evaluates the program, asserts the completion value is callable, and calls it with the event object (the same field shape as the command stdin JSON); a non-callable completion (a deleted or altered wrapper) is a handler failure, never silently allowed. The script compiles once per run (cached on the resolved hook); a return of `{decision: "block", reason}` blocks through the D3 shape, ANY other return (undefined, `{}`, prose) allows — stricter than D10's whole-stdout rule, with no parse-ambiguity channel at all — and an uncaught exception is a failure → `on_failure`. The VM binds NOTHING — no network, filesystem, environment, or process globals — so the lane is pure computation over the event: the *decide* lane versus command's *do* lane, and the reason script hooks are EXEMPT from D12's matcher requirement (in-process µs-cheap; D14's "script gate above the LLM evaluator" drag ordering lands here). Budget: `vm.Interrupt` at the hook's `timeout_ms` plus a goja runtime memory limit as the allocation backstop — the SIGTERM/SIGKILL teardown machinery disappears entirely. `console.log`/`console.error` are captured into a capped buffer surfaced by the D18 Test panel (printf-debugging without stderr). Save validation first requires the wrapper shape — the source must open with `(function(input){` and close with `})` (tolerant of whitespace/prettier spacing and a trailing semicolon), so a deleted or altered wrapper fails at save with the keep-the-wrapper message, never silently allows — then compiles (`goja.Compile`) WITHOUT running → 422 naming the first syntax error's line/column at its true coordinates (the editor displays exactly this text). Audit and error text carry hook name + error, never the script body. Kill switch `ONCLAW_HOOKS_SCRIPT_ENABLED`, default on (symmetry with D10; strictly the safest handler).

**D23 — Script editor: parse-validated with one-click Format (post-implementation amendment).** The `script` field is a real editor, not a bare textarea (user bar: "beautify and mini lsp to make sure the syntax correct"): CodeMirror 6 (`@codemirror/lang-javascript` + `@codemirror/lint`) provides JS highlighting, line numbers, and inline squiggles with line/column from the Lezer parse while the author types — parse-diagnostics-as-you-type, no language server ("mini lsp"). A parse error BLOCKS save client-side, mirroring the server's D22 422. A Format button beautifies the buffer in place via Prettier standalone (`prettier/standalone` + babel/estree plugins). Switching the handler to `script` seeds an EMPTY buffer with a starter template showing the full `(function(input){ … })` wrapper (the wrapper is harness structure, displayed in the editor and stated as such in the helper text: edit only the body), a minimal allow-return the author edits into their logic (the helper also states the engine targets ES5.1 only); a buffer that already holds text (existing hook, or a round-trip away and back) is never overwritten. Both dependency trees are lazy-loaded (dynamic import scoped to HookDialog open / first Format click) so the main chunk never grows. Rejected: Monaco (~2 MB + web-worker machinery for one field); bare textarea + acorn (no highlighting, no gutter — authoring quality is the point of the feature).

## UI Contract (ASCII, post-amendment)

The binding UI shape for the D19–D21 amendment. The dialog's Matcher section collapses to ONE text input (the Segmented Structured/Regex control, the mode dropdown, and the chips from the original build are removed); the `if` field appears only on tool events; handler sections speak the CC key names. The D22–D23 amendment adds the `script` handler section at the end of this gallery.

### HookDialog — full view (pre_tool_use + command handler)

```
┌────────────────────────────────────────────────────────────────────┐
│  Edit hook                                                      ✕  │
│                                                                    │
│  Name                                                              │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ policy-guard                                                 │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  Event                                                             │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ pre_tool_use — can block                                  ▼  │ │
│  └──────────────────────────────────────────────────────────────┘ │
│  Fires before every tool call — can block individual calls.        │
│                                                                    │
│  Matcher                                    Matches 2 of 24 tools  │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ shell, read_file                                             │ │
│  └──────────────────────────────────────────────────────────────┘ │
│  Empty or * = every occurrence · names and families (mcp__github.*)│
│  split on , | space match exactly · anything else is an unanchored │
│  regex                                                             │
│                                                                    │
│  If (optional)                                                     │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ read_file(secret*)                                           │ │
│  └──────────────────────────────────────────────────────────────┘ │
│  Claude Code-style input gate — runs only when the tool name       │
│  matches and the pattern hits its input JSON; otherwise the hook   │
│  is skipped silently. Format: Tool(pattern).                       │
│                                                                    │
│  Handler                                                           │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ command — local program                                   ▼  │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  Command                                                           │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ /usr/local/bin/gate                                          │ │
│  └──────────────────────────────────────────────────────────────┘ │
│  Exec form on the OnClaw host — the event JSON arrives on stdin;   │
│  exit 2 blocks.                                                    │
│                                                                    │
│  Arguments                                                         │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ --strict --policy ops                                        │ │
│  └──────────────────────────────────────────────────────────────┘ │
│                                                                    │
│  Timeout (ms)        On failure        Enabled                    │
│  ┌──────────────┐   ┌───────────────┐   ┌───┐                      │
│  │ 5000         │   │ ◉ Allow       │   │ █ │                      │
│  └──────────────┘   │ ○ Block       │   └───┘                      │
│                     └───────────────┘                               │
│                                                                    │
│                                          [ Cancel ]  [ Save hook ] │
└────────────────────────────────────────────────────────────────────┘
```

The same Matcher field under the regex tier — no mode switch anywhere; the live count tells the author which reading is active:

```
│  Matcher                                     Matches 2 of 24 tools │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │ ^web\.                                                       │ │
│  └──────────────────────────────────────────────────────────────┘ │
```

### Handler sections — CC key names

```
  ┌ mcp_tool ─────────────────────────────────────────────────────┐
  │  Server                                                       │
  │  ┌───────────────────────────────────────────┐                │
  │  │ github                                 ▼  │                │
  │  └───────────────────────────────────────────┘                │
  │  Workspace MCP servers only — agent-private are unreachable.  │
  │                                                               │
  │  Tool                                                         │
  │  ┌─────────────────────────────────────────────────────────┐  │
  │  │ create_issue                                            │  │
  │  └─────────────────────────────────────────────────────────┘  │
  │                                                               │
  │  Input                                                        │
  │  ┌─────────────────────────────────────────────────────────┐  │
  │  │ { "title": "[${event.origin}] ${event.agent.name}" }    │  │
  │  └─────────────────────────────────────────────────────────┘  │
  │  ${event.*} placeholders are substituted before the call.     │
  └───────────────────────────────────────────────────────────────┘

  ┌ prompt ───────────────────────────────────────────────────────┐
  │  Prompt                                                       │
  │  ┌─────────────────────────────────────────────────────────┐  │
  │  │ Block any call whose args contain credentials or keys.  │  │
  │  └─────────────────────────────────────────────────────────┘  │
  │  Evaluator sees the tool name + delimited args only — never   │
  │  the conversation. It must call decide(); free text is        │
  │  ignored; an injection verdict always blocks.                 │
  │                                                               │
  │  Evaluator model                                              │
  │  ┌──────────────────────┐   ┌──────────────────────┐          │
  │  │ openai            ▼  │   │ gpt-4o-mini       ▼  │          │
  │  └──────────────────────┘   └──────────────────────┘          │
  │  Max invocations per run                                      │
  │  ┌──────────────┐                                             │
  │  │ 5            │   over cap → allow, recorded as capped      │
  │  └──────────────┘                                             │
  └───────────────────────────────────────────────────────────────┘

  ┌ script — JavaScript, in-process ──────────────────────────────┐
  │  Script                                             [Format]  │
  │  ┌─────────────────────────────────────────────────────────┐  │
  │  │ 1 │ (function (input) {                                 │  │
  │  │ 2 │   return { decision: "allow", reason: "" };         │  │
  │  │ 3 │ });                                                 │  │
  │  └─────────────────────────────────────────────────────────┘  │
  │  The (function(input){ … }) wrapper is the harness structure  │
  │  — edit only the body.                                        │
  │  Syntax errors surface inline as you type; a parse error      │
  │  blocks save. Format beautifies in place (Prettier).          │
  │                                                               │
  │  Runs inside OnClaw — no network, files, or env access.       │
  │  Return {decision, reason} to block; anything else allows.    │
  │  ES5.1 only — newer syntax (const, arrow functions, template  │
  │  literals) is not supported.                                  │
  └───────────────────────────────────────────────────────────────┘
```

### Non-tool events — matcher reads origins/statuses, `if` hidden

```
  run_started                                    run_finished
├─────────────────────────────────────┐   ├─────────────────────────────────────┤
│  Matcher      Matches 2 of 3 origins│   │  Matcher    Matches 1 of 3 outcomes │
│  ┌────────────────────────────────┐ │   │  ┌────────────────────────────────┐ │
│  │ user|cron                      │ │   │  │ failed                         │ │
│  └────────────────────────────────┘ │   │  └────────────────────────────────┘ │
│  (same syntax helper)               │   │                                     │
│  Handler  …                         │   │  Handler  …                         │
└─────────────────────────────────────┘   └─────────────────────────────────────┘
  no If field — only pre_tool_use and post_tool_use accept one
```

### Settings → Hooks pane — rows carry the plain string

```
  SETTINGS · Hooks                                            [ New hook ]
  ────────────────────────────────────────────────────────────────────
  Workspace hooks — evaluate top to bottom, first block wins.
  ┌──────────────────────────────────────────────────────────────────┐
  │ ⠿ ● policy-guard      pre_tool_use · shell|read_file             │
  │     command · /usr/local/bin/gate                    ● ok    ⋯  │
  ├──────────────────────────────────────────────────────────────────┤
  │ ⠿ ● cron-watcher      run_started · user|cron                    │
  │     http · https://hooks.example.com                 ● ok    ⋯  │
  ├──────────────────────────────────────────────────────────────────┤
  │ ⠿ ● failure-pager     run_finished · failed                      │
  │     command · /opt/onclaw-hooks/pager.sh             ● ok    ⋯  │
  └──────────────────────────────────────────────────────────────────┘
        ⋯ opens the row menu: Test · History · Edit · Disable · Delete

  INSTANCE — read-only, mandatory, applies to every workspace
  ┌──────────────────────────────────────────────────────────────────┐
  │ ○ org-policy-gate     pre_tool_use · *        managed · ok       │
  └──────────────────────────────────────────────────────────────────┘

  RECENT EXECUTIONS (workspace-wide, latest 20)
  ┌──────────────────────────────────────────────────────────────────┐
  │ policy-guard · pre_tool_use · block · read_file · 4 ms · 2m ago  │
  │ cron-watcher  · run_started  · allow · cron    · 88 ms · 1h ago  │
  └──────────────────────────────────────────────────────────────────┘
```

The Test panel and per-hook history are unchanged from the original build (decision badge, duration, exit code / HTTP status / tokens, no-audit note); the agent modal's Hooks section keeps its read-only instance/workspace visibility with the matcher rendered as the plain string.

## Risks / Trade-offs

- [Blocking hooks tax the hot path — every tool call pays up to `timeout_ms` per matching hook, sequentially] → Matcher filtering runs before any dispatch; timeout defaults are conservative (5 s; 15 s for prompt); ordering lets operators put cheap gates first; no chain budget in v1 (documented; a per-run cap can follow).
- [A dead webhook endpoint silently disables a policy gate] → `status` column + execution history + Test button; save-time match-count; LookPath warnings. (Deliberately not a circuit breaker in v1.)
- [Command hooks run as the server user on the host] → Same trust level as existing stdio MCP servers; exec form + env containment + kill switch; matcher is the frequency control, not a sandbox.
- [Prompt hooks cost tokens per matching call] → Matcher required, per-run invocation cap, explicit model, `token_count` audit; budgets deferred until audit data justifies the design.
- [Rolling deploys create definition/code skew] → Per-run reads converge content immediately; the skip-what-you-cannot-parse rule makes skew non-fatal; version guard blocks downgrades; removal converges one rolling cycle late (accepted).
- [instance_hooks bends the workspace-scoping rule] → Single, explicitly commented exception, reachable only via the instance-admin permission surface.
- [Regex matchers can over- or under-match silently] → RE2 + save-time match-count + Test dry-run; anchoring left raw for power users (picker covers the safe path).

## Migration Plan

1. Migration up creates the four tables (+ permission backfill in the same release); `migrate down` reverses. No existing rows are rewritten; `agents` gains no columns, so rollback is trivial.
2. Deploy order: `migrate up` once (pipeline, golang-migrate lock), then roll pods — `SyncBuiltinHooks` (v1: empty set) runs per pod at startup; no coordination needed.
3. Rollback: `migrate down` removes hook state (audit history lost — accepted, feature is new); the binary without the feature simply ignores the tables.
