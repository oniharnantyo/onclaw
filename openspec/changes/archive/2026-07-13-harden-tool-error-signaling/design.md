## Context

The agent loop is ReAct-style: every tool call MUST produce an observation the model can reason
about, or the loop has nothing to continue with. Eino enforces this strictly — a tool returning a
non-nil `error` is **fatal** (verified at both code paths in `compose/tool_node.go`: streaming
`:1203`, blocking `:1102`); the only tool error that survives is a special `InterruptAndRerunError`.
Every other error becomes a graph-node failure → agent stream `event.Err` → `eventIterator` fires
`EventStop` → the turn and the streaming session terminate.

The bug is systemic. These tools return Go errors for **expected** conditions, killing the session:

- `fsBackend` (`read_file`/`write_file`/`edit_file`/`ls`/`glob`/`grep`): path-traversal, not-found,
  non-unique edit, invalid/empty regex/glob.
- `memory` tool (`WriteCore`): replace/remove target not found or not unique, unknown operation,
  character-limit-exceeded (the message even contains recovery guidance the model never sees).
- `kg_search`: empty `seed_entity_name` (input validation).
- `web_fetch` / `web_search`: network/HTTP/provider failure, including the terminal case where the
  default fallback provider also fails.
- All eleven browser tools: no active page/engine, navigation timeout, element-not-found,
  action/JS-eval failure (bare `return "", err`).

The `execute` shell backend (`fsShell.Execute`) does NOT have this bug: it folds every policy block
and every non-zero exit into the response `Output` with `nil` error, and treats only context
cancellation specially — this is the reference pattern.

Some specs already mandate recovery. The browser spec ("Browser tools degrade safely without an
engine": *"the agent continues"*) and the web spec ("fall back to defaults … return a result rather
than a hard failure") already describe the desired behavior; the implementation contradicts them.

## Goals / Non-Goals

**Goals:**

- **Every** builtin tool returns expected failures as tool-result observations (nil error) so the
  turn continues and the model can adapt.
- Infrastructure failures remain fatal Go errors.
- Context cancellation is propagated, never converted.
- Uniform *contract* across all tools, with the *mechanism* chosen per family.
- Preserve access-control policy exactly (path confinement, SSRF) — only the signaling channel
  changes.

**Non-Goals:**

- Changing the workspace-confinement *policy* (whether `/tmp`, `$HOME`, etc. are reachable) — a
  separate design question.
- Touching the `execute` shell tool — already correct.
- MCP tools — external, out of scope.
- Forking/vendoring the Eino filesystem middleware.
- Changing `ValidatePath`'s signature or its non-filesystem callers.

## Decisions

### Decision 1: Two mechanisms, chosen per tool family

The fix mechanism is dictated by each tool's invocation shape:

- **Filesystem tools → tool-invocation middleware (`FSErrorMiddleware`).** The `filesystem.Backend`
  interface is asymmetric: `Read`/`LsInfo`/`GrepRaw`/`GlobInfo` return `(result, error)` and could
  embed a decline-message, but `Write` and `Edit` return **only `error`** — they cannot express
  "declined" non-fatally (returning `nil` reads as success). A fix confined to `fsBackend` cannot
  cover Write/Edit. The middleware (sibling to `FSToggleMiddleware`) sees the full `(string, error)`
  tool return uniformly across all six tools.

- **`memory`, `kg_search`, `web_*`, `browser_*` → direct call-site conversion.** These are built
  with `utils.InferTool`; their invoke funcs return `(string, error)` directly, so each expected-
  failure site converts `return "", err` → `return "<observation>", nil` inline. No middleware
  required. **Caveat:** `memory` *does* declare package-level sentinels in `internal/memory`
  (`ErrTargetNotFound`, `ErrTargetNotUnique`, `ErrTargetRequired`, `ErrUnknownOp`,
  `ErrCharLimitExceeded`); `memory.go` classifies the `WriteCore` error with `errors.Is` and returns
  the sentinel's message verbatim as the observation. `kg_search` / `web_*` / `browser_*` use
  ad-hoc inline observations without sentinels. A small shared helper
  (`tools.ObservationFor(kind, ...)`) may reduce duplication but is optional.

**Rationale.** The contract is universal; the mechanism follows the interface constraint. Using the
middleware only where the interface forces it, and direct conversion elsewhere, keeps each fix
local and obvious. Trying to route all tools through one middleware would require every tool to
emit sentinels — unnecessary indirection for tools that can simply return a string.

**Alternatives considered.**

- *Convert inside `fsBackend` for the `(result, error)` methods only.* Rejected: leaves Write/Edit
  session-killing, and splits one concern across two mechanisms.
- *String-match error messages in the middleware.* Rejected: fragile (`[LocalFunc]` wrapping and
  wording changes silently break classification).
- *Route all tools through `FSErrorMiddleware` with sentinels everywhere.* Rejected: needless
  sentinel ceremony for tools whose invoke func already returns a string.

### Decision 2: Sentinel classification for the filesystem middleware

For the filesystem family only, declare sentinel errors in a new `internal/agent/tools/errors.go`
(`ErrPathOutsideWorkspace`, `ErrFileNotFound`, `ErrPermissionDenied`, `ErrEditNotUnique`,
`ErrEditOldStringMissing`, `ErrEmptyPattern`, `ErrInvalidRegex`, `ErrInvalidGlob`). `fsBackend`
returns these (wrapped `%w` + `: %q` for the offending path/value). The middleware converts any
error matching the set via `errors.Is` to a result string; everything else stays fatal.

`ValidatePath` stays a pure function returning a plain error (its other callers are unaffected);
`fsBackend` wraps its result into `ErrPathOutsideWorkspace` at the call site. Eino preserves the
error chain (`invokable_func.go:199` uses `%w`), so `errors.Is` matches through the wrapping.

### Decision 3: The decline-vs-fail classification (all families)

| Condition | Family | Class | Signal |
|---|---|---|---|
| Path resolves outside workspace | fs | expected | observation |
| File / dir not found (`os.IsNotExist`) | fs | expected | observation |
| Permission denied on a workspace path | fs | expected | observation |
| `edit_file`: old_string empty / missing / non-unique | fs | expected | observation |
| `grep`/`glob`: empty/invalid pattern | fs | expected | observation |
| `memory`: target not found / not unique / unknown op | memory | expected | observation |
| `memory`: write would exceed character limit | memory | expected | observation (message includes guidance) |
| `kg_search`: empty required field | memory | expected (input validation) | observation |
| `web_fetch`/`web_search`: network/HTTP/provider failure | web | expected (transient) | observation |
| `web_*`: default fallback provider also fails | web | expected | observation |
| Browser: no active page / engine unavailable | browser | expected | observation |
| Browser: nav timeout / element-not-found / action failure | browser | expected (transient) | observation |
| Context cancelled / deadline exceeded | all | propagate | fatal (never converted) |
| Any other unrecoverable I/O / disk error | all | infrastructure | fatal |

**Rationale.** "Expected" = the tool declining, not finding, or hitting a transient external
condition — normal agent operation the model can recover from. "Infrastructure" = the world is
broken — fatal so it isn't masked.

### Decision 4: Context-cancellation guard at every conversion site

Every conversion (middleware and direct) MUST check `ctx.Err()` first and return the error
unchanged when the context is cancelled/deadlined. Without this, a user cancellation could be
converted to an observation and the turn would continue after the user stopped it. This mirrors
`fsShell.Execute`'s existing `ctx.Err()` handling.

### Decision 5: Result-message content discipline

Observations name the **policy/reason** and the **requested path/value/URL**, following
`ValidatePath`'s discipline of quoting only the requested input — never the absolute workspace
root, never probing filesystem state beyond what the condition established, and never leaking
SSRF-internal resolution details. For the `memory` char-limit case, the observation includes the
recovery guidance ("consolidate or delete old memories first") so the model can act on it.

## Risks / Trade-offs

- **Masking real bugs by over-converting** → Mitigation: the middleware's default arm keeps unknown
  errors fatal; direct conversions are inline at sites that *know* the condition. Adding a new
  expected condition is a deliberate change, not a catch-all.
- **Model retries the same failing operation until `max_iterations`** → Mitigation: observations are
  specific so the model can choose differently; bounded by `max_iterations`; strictly better than
  dying on iteration 1. Highest risk on flaky web/browser tools — acceptable, since retry-with-
  backoff is the correct agent behavior there.
- **Browser flakiness converted to noisy observations** → Mitigation: observations carry the
  underlying reason so the model can decide to re-snapshot, re-navigate, or report to the user.
- **A future expected failure stays fatal because no one converted it** → Mitigation: regression
  tests assert the known-good cases recover; the universal contract in the `agent-tools` spec makes
  the expectation explicit for future tools.
- **Enhanced (multimodal) `read_file` path** → Mitigation: `WrapEnhancedInvokableToolCall` is
  handled too (mirroring `FSToggleMiddleware`), so enabling `UseMultiModalRead` later does not
  reintroduce the bug.

## Migration Plan

- Purely behavioral; no storage/config/API change. Revert the commit to roll back.
- Order: (1) filesystem family — sentinels + `fsBackend` + `FSErrorMiddleware` + wiring; (2) memory
  + kg — direct conversion; (3) web — direct conversion; (4) browser — direct conversion; (5) tests
  + behavioral verification across all families.

## Open Questions

- Permission-denied (`os.IsPermission`) — observation or fatal? Design leans observation; confirm
  during implementation.
- For web/browser, should retryable failures (timeout, 5xx) be flagged distinctly in the observation
  ("transient — you may retry") to guide the model? Design leans a terse reason only.
