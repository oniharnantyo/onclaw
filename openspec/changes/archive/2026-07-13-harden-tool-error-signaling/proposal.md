## Why

Builtin tools return **fatal Go errors** for **expected, recoverable** conditions, and the Eino
agent loop treats every non-`InterruptRerun` tool error as terminal — so the agent turn and the
streaming session die whenever a tool merely *declines*, *doesn't find* something, or hits a
transient external failure. This is systemic, not isolated: `read_file`/`write_file`/`edit_file`/
`ls`/`glob`/`grep` (path-traversal, not-found, non-unique edit, bad pattern), the `memory` tool
(target-not-found, target-not-unique, unknown-op, character-limit-exceeded), `kg_search` (empty
required field), `web_fetch`/`web_search` (network/HTTP/provider failure), and all eleven browser
tools (no-active-page, navigation timeout, element-not-found, action failure) all kill the session
instead of returning an observation the model can read and recover from. The shell `execute` tool
is the one builtin that already does this correctly and is the reference pattern.

Notably, some existing specs already *mandate* recovery (the browser spec: "the agent continues"
on engine-unavailable; the web spec: "return a result rather than a hard failure") — the
implementation contradicts them. This change enforces a single, uniform decline-vs-fail contract
across every builtin tool.

## What Changes

- Every builtin tool SHALL return **expected-failure** conditions as **tool-result observations**
  with `nil` error, so the agent turn continues and the model can adapt. This covers four classes:
  1. **Input validation** — missing/invalid arguments (e.g. `kg_search` empty `seed_entity_name`,
     `memory` unknown op).
  2. **Resource state** — not-found / not-unique / would-exceed-limit (e.g. `edit_file`, the
     `memory` tool's replace/remove target, the MEMORY.md character limit).
  3. **External/transient failure** — network/HTTP/provider/rate-limit failures in `web_fetch` and
     `web_search`, including the terminal case where the default provider also fails.
  4. **Browser automation failure** — no active page/engine, navigation timeout, element-not-found,
     action/JS-eval failure, across all browser tools.
- Only **genuine infrastructure failures** (unrecoverable I/O, etc.) SHALL remain fatal Go errors.
- **Context cancellation** (`context.Canceled`, `context.DeadlineExceeded`) SHALL be propagated and
  never converted to an observation.
- Access-control policy is unchanged: path confinement (`ValidatePath`) and SSRF protection still
  block; only the *signaling channel* of the block changes, from fatal error to recoverable
  observation.
- Scope is all builtin tools. The shell `execute` tool already follows the contract and is
  unchanged. MCP tools are out of scope (external).

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `agent-tools`: File-tool rejections become recoverable observations; a new cross-cutting
  requirement establishes the decline-vs-fail contract for **all** builtin tools. Disambiguates
  current wording ("returns a blocked error") that is compliant under both the fatal and the
  recoverable reading.
- `agent-memory`: The `memory` tool's decline conditions (target not found / not unique, unknown
  operation, character-limit-exceeded) SHALL be observations, not fatal errors.
- `web-tools`: `web_fetch` and `web_search` SHALL return a fetch/search failure — including the
  terminal case where the default provider also fails — as an observation rather than a fatal error.
- `browser-tool`: All browser tools SHALL return runtime/engine failures as observations so the
  agent continues, bringing the implementation into compliance with the existing "degrade safely
  without an engine" requirement and extending it to runtime failures.

## Impact

- **Code:**
  - `internal/agent/tools/fsbackend.go` + new `errors.go` sentinels + new
    `internal/agent/middlewares/fs_error_middleware.go` (filesystem tools need the middleware
    approach because the `Backend.Write`/`Edit` interface returns only `error`).
  - `internal/agent/tools/memory.go` (`memory` tool), `kg_search.go` (input validation),
    `web/webfetch.go`, `web/websearch.go`, and `internal/agent/tools/browser/*.go` (all ops) —
    direct call-site conversion of expected failures to observations (these tools' invoke funcs
    return `(string, error)` directly, so no middleware is needed).
  - `internal/agent/agent.go` — wire `FSErrorMiddleware` next to `fsToggle`.
  - No change to `ValidatePath`, the Eino middleware (upstream), the agent loop, or the shell tool.
- **Specs:** `agent-tools`, `agent-memory`, `web-tools`, `browser-tool` gain decline-vs-fail
  requirement deltas.
- **Behavior:** Agent turns no longer terminate on a blocked / missing / ambiguous / network /
  browser operation. The model receives the reason as an observation and continues within its
  `max_iterations` budget.
- **Security:** No change. Confinement (path + SSRF) is still enforced; only the failure-signaling
  channel changes, which is orthogonal to access control.
