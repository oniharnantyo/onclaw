## 1. Schema, domain, permissions

- [x] 1.1 Write migration `000026_agent_hooks`: `instance_hooks` (key, source builtin|managed, version, event, matcher JSONB, handler_type, config JSONB, timeout, on_failure, enabled, position, status, unique (source, key)), `workspace_hooks` (workspace FK, name, event, matcher JSONB, handler_type, config JSONB, timeout, on_failure, enabled, position, status, unique (workspace_id, name)), `agent_hooks` (agent FK cascade + workspace FK, same shape), `hook_executions` (nullable hook id ON DELETE SET NULL + denormalized hook name/level, workspace id, event, decision, duration, failure detail columns, token_count nullable) — with the workspace-unscoping comment on `instance_hooks`; `migrate down` reverses
- [x] 1.2 Add permission catalog entries `hooks.read` / `hooks.write` and an idempotent backfill migration granting them to built-in Superadmin/Owner/Admin roles (000022 pattern); verify with integration test
- [x] 1.3 Add `internal/domain/hooks.go`: hook domain types for the three levels, `HookMatcher` tagged union {Type tools|regex; Mode all|except|only; Tools; Pattern}, handler config shapes per type, validation rules (event whitelist, matcher coherence, RE2 compile + 256 cap, tools entry charset + trailing-`.*` only, unknown tool names allowed-but-flagged, prompt handler requires non-match-all matcher, mcp_tool requires a workspace server, env/header name charset)
- [x] 1.4 Extend the store aggregate with hook store sub-interfaces (per-level CRUD + ordered reposition + execution recording/lookup) and implement them in the in-memory fake

## 2. Matcher

- [x] 2.1 Implement `internal/agents/hooks/matcher.go`: `Matcher interface{ Matches(value string) bool }` and `CompileMatcher` — exact + trailing-`.*` prefix for tools mode (all/except/only), RE2 `regexp.MatchString` for regex mode; compile once per run
- [x] 2.2 Table-driven matcher tests: exact, prefix families (`browser.*`, `mcp__github__*`), except-negation, match-all, unanchored regex (`web` selects `web.fetch`), status/origin value sets
- [x] 2.3 Implement the event-aware candidate mapping (tool name | origin | status) and the workspace-visible value enumeration for the match-count report (registry names + browser alias expansion + workspace MCP tools; fixed enums for origin/status); unit-test the count math

## 3. Handler registry

- [x] 3.1 Implement `internal/agents/hooks/handler.go`: `Handler interface{ Execute(ctx, Event, budget) (Decision, error) }`, the registry with `http`/`command`/`mcp_tool`/`prompt` registrations, and the shared decision contract (success without decision = allow)
- [x] 3.2 Implement the `http` handler: POST event JSON with event/delivery headers, configured encrypted headers, decision-body parsing, non-JSON 2xx = allow; reuse the webfetch outbound guard (loopback/private/link-local + redirect blocking); contract tests
- [x] 3.3 Implement the `command` handler per the decision table: exec form, stdin JSON (64 KB string truncation), strict whole-stdout JSON rule, exit 0/2 semantics with exit-2 precedence over stdout JSON, truncated-stderr reason, other exits → failure; concurrent pipe drain; SIGTERM → 2 s → SIGKILL teardown; minimal env (PATH/LANG/TMPDIR + hook rows); `ONCLAW_HOOKS_COMMAND_ENABLED` gate; contract tests covering every decision-table row
- [x] 3.4 Implement the `mcp_tool` handler: workspace-server resolution, closed `${event.*}` placeholder substitution, decision JSON from text result, error → failure; tests incl. cold-connect error path
- [x] 3.5 Implement the `prompt` handler: sandboxed evaluator prompt (tool name + delimited untrusted args only), forced `decide(decision, reason, injection_detected)` tool, missing verdict = failure, injection flag = block, explicit provider/model via evaluator model factory, per-run invocation cap (over cap → allow + recorded), token counting; tests incl. free-text-response and cap-exceeded paths

## 4. Dispatcher

- [x] 4.1 Implement the dispatcher: per-run resolve across the three levels in tier order (instance → workspace → agent) preserving list position, matcher compilation, graceful skip of un-interpretable hooks (unknown handler type/event/uncompilable matcher → warning + status error + on_failure policy)
- [x] 4.2 Implement the dispatch loop: matcher skip (no execution record), ordered execution with per-hook timeout, failure → on_failure policy, first block short-circuits blocking seams, audit row + status update per execution
- [x] 4.3 Implement call-ID dedup so an approved tool call re-executing after human approval does not re-fire `pre_tool_use` within the run
- [x] 4.4 Implement detached-context execution for observational events (context.WithoutCancel + own budget + panic recovery) and the run_finished-after-terminal ordering (cancel path after the drain settles)
- [x] 4.5 Wire `WithHooks` as a Runner functional option with a no-op default; keep the composition root as the only place real dependencies are injected

## 5. Runtime seams

- [x] 5.1 Add `hooksMiddleware` to `buildMiddlewares` wrapping BOTH `WrapInvokableToolCall` and `WrapStreamableToolCall` (pass-through for non-matching hooks; block = `{"blocked_by_hook", "reason"}` tool result; post-tool tap of stream frames without buffering); append it before the tool-error-result middleware; regression-test that interrupts/cancellations pass through untouched
- [x] 5.2 Add the run-entry seam: `user_prompt_submit` evaluation before the model with block = notice event + well-formed `turn_completed` terminal + persisted history entry; `run_started` observer fire
- [x] 5.3 Add the terminal seam: `run_finished` with `status` completed|failed|cancelled, fired after the terminal transcript event settles (cancel path after the durable cancel marker drains)
- [x] 5.4 Add transcript event kind(s) for hook enforcement (blocked tool card payload, blocked-prompt notice) and persist them so hydrated transcripts render identically; history round-trip tests
- [x] 5.5 Runner integration tests: blocked tool call continues the run with the block result; blocked prompt never reaches the model; observer slowness/panic leaves the run untouched; resume dedup

## 6. Builtin instance sync

- [x] 6.1 Implement the builtin registry (embedded slice: stable key, version int, definition) and `SyncBuiltinHooks` in `internal/bootstrap` — upsert with version guard (`ON CONFLICT (source,key) ... WHERE version < EXCLUDED.version`), removal deletion, `hook_executions` preservation via SET NULL + denormalized name; v1 slice is empty
- [x] 6.2 Postgres implementation for all hook stores (workspace-scoped queries for workspace/agent levels; instance queries only in the instance surface); integration tests incl. concurrent-start upsert race and version-guard no-downgrade

## 7. REST API

- [x] 7.1 Workspace hooks endpoints: CRUD + reorder + enable/disable, matcher validation with match-count in the save response (422 details[] on failure), secret keep-stored merge on update, execution history listing, permission gating (`hooks.read`/`hooks.write`) and error envelopes per the API-error spec; handler tests
- [x] 7.2 Instance admin surface: superadmin CRUD for managed instance hooks + read-only builtin listing, under the instance-admin permission surface; handler tests
- [x] 7.3 Hook test endpoint: executes the handler against a synthetic event, returns decision/duration/handler detail, writes no audit row; handler tests
- [x] 7.4 Router registration + smoke coverage in `scripts/smoke.sh` (CRUD, validation 422, permission 403, test dry-run, history)

## 8. Web UI

- [x] 8.1 Settings Hooks pane: ordered list with drag repositioning, health status, enable toggle, handler/level chips, read-only instance section, "top to bottom — first block wins" helper
- [x] 8.2 Hook dialog: event select with fires-on helper; event-aware applies-to editor (structured picker over tools/origins/statuses + advanced regex field) with live match count; handler sections swapping by type reusing MCP components (masked keep-stored rows, provider/model picker, structured input rows); timeout + failure-policy radio + enabled
- [x] 8.3 Test panel (decision badge, duration, exit code / HTTP status / tokens, payload preview, no-audit note) and per-hook execution history view
- [x] 8.4 Agent modal Hooks section: agent-level hook CRUD + read-only instance/workspace visibility with level badges
- [x] 8.5 Chat transcript rendering for blocked tool cards and blocked-prompt notices, live and hydrated; jsdom tests for the new components (mock every select option tests exercise)

## 9. Verification

- [x] 9.1 `go build ./...`, `go vet ./...`, `go test ./...` green; `go test -tags=integration ./...` green with DATABASE_URL
- [x] 9.2 Full `./scripts/smoke.sh` pass including the new hooks section
- [x] 9.3 Update AGENTS.md zero-credential/feature notes if affected (hooks capability entry) — docs only, no behavior
- [ ] 9.4 Manual browser pass: create each handler type, test dry-run, watch a live blocked tool call and a blocked prompt, drag-reorder, reload transcript

## 10. Claude Code parity (post-implementation amendment: D19–D21)

- [x] 10.1 Domain: `HookMatcher` tagged union → single `Matcher string` (JSON key `matcher`); rewrite matcher validation to the D19 tiered reading (list tier charset, regex tier RE2 + 256 cap); adjust the prompt-handler non-match-all rule (non-empty, not `*`); update domain tests
- [x] 10.2 Migration `000029_matcher_string`: `matcher` JSONB → text on all three hook tables with one-time conversion (`only` → `a|b`, `except` → match-all, regex → pattern); `migrate down` reverses
- [x] 10.3 Store: postgres `if_rule` plumbing for 000028 (column lists, scans, inserts/updates, builtin upsert) + matcher-as-string scan/marshal; fake store parity; integration tests for matcher round-trip, `if` round-trip, and the 000029 conversion
- [x] 10.4 Runtime: `CompileMatcher` rewrite to the tier machine (compile once per run; uncompilable → D7 graceful skip); `CountMatches` over the string form; dispatcher `if` evaluation after the matcher skip (skip, no audit row, split at first `(`); config key renames `server`/`tool` (mcp_tool) and `prompt` (prompt handler) incl. the secrets doc comment; unit tests for tiers, `if` gating, renames
- [x] 10.5 REST + smoke: `matcher` string and `if` through request/response and match-count paths; handler tests updated; `scripts/smoke.sh` section 16 payloads updated
- [x] 10.6 Web: HookDialog matcher → one text input with live count + syntax helper; optional `if` field on tool events only (per the ASCII contract); server/tool/prompt labels and keys; `api.ts` + `hooksUi` summary/count rewrites; jsdom tests
- [x] 10.7 Gates: `go build`/`vet`/`test`/integration green; full `./scripts/smoke.sh` green incl. section 16; `pnpm build` + touched suites green
- [x] 10.8 UI alignment pass: walk every hooks surface (HookDialog, pane rows, agent-modal Hooks section, test panel, history) against the design.md **UI Contract (ASCII)** gallery and fix deviations — the single-input Matcher with live count and syntax helper replacing the transient Segmented/mode/chips build, the optional `if` field on tool events only, `server`/`tool`/`prompt` labels and keys, row summaries carrying the plain string; jsdom tests updated to the new testids

## 11. Script handler (post-implementation amendment: D22–D23)

- [x] 11.1 Domain + deps: `github.com/dop251/goja` in go.mod; `script` handler type in the domain catalog with config validation (`config.script` required non-empty, byte cap ~64 KB); script hooks exempt from the prompt-handler non-match-all matcher rule; kill switch `ONCLAW_HOOKS_SCRIPT_ENABLED` plumbed like D10's
- [x] 11.2 Runtime `script.go`: compile once per run, invoked as `(function(input){ … })` — `{decision, reason}` return honored through the D3 block shape, any other return allows, uncaught exception → `on_failure`; `vm.Interrupt` at `timeout_ms` + runtime memory limit; capped `console.log`/`console.error` capture returned to the Test panel; audit/error text never echoes the script body; unit tests (block / allow / throw / timeout / console / sandbox: no network-fs-env-process globals)
- [x] 11.3 Save-time validation + API: `goja.Compile` without execution → 422 naming the first syntax error's line/col; REST handler tests incl. matcher-not-required; smoke section-16 script-hook payloads (CRUD + dry-run block with console output in the test result)
- [x] 11.4 Web: HookDialog `script — JavaScript, in-process` handler section per the D23 gallery — CodeMirror 6 editor (lang-javascript + lint, line numbers, inline parse squiggles), parse error blocks save client-side, lazy-loaded Format button (Prettier standalone, babel+estree) beautifying in place, Test panel shows captured console output; `api.ts` handler type + testids + jsdom tests (editor renders, parse error blocks submit, format handler)
- [x] 11.5 Gates: `go build`/`vet`/`test`/integration green; full `./scripts/smoke.sh` green; `pnpm build` + touched suites green
