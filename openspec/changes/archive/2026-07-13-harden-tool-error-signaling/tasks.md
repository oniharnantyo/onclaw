## 1. Filesystem mechanism — sentinels and fsBackend

- [x] 1.1 Create `internal/agent/tools/errors.go` declaring sentinel errors: `ErrPathOutsideWorkspace`, `ErrFileNotFound`, `ErrPermissionDenied`, `ErrEditNotUnique`, `ErrEditOldStringMissing`, `ErrEmptyPattern`, `ErrInvalidRegex`, `ErrInvalidGlob`. Wrap the offending path/value via `%w` + `: %q`.
- [x] 1.2 In `fsBackend`, wrap every `ValidatePath` error into `ErrPathOutsideWorkspace` at each call site (`Read`, `Write`, `Edit`, `LsInfo`, `GrepRaw`, `GlobInfo`). Leave `ValidatePath` itself unchanged.
- [x] 1.3 Map OS errors in `fsBackend`: `os.IsNotExist` → `ErrFileNotFound`, `os.IsPermission` → `ErrPermissionDenied` at the `os.ReadFile`/`os.ReadDir`/`os.WriteFile`/`os.Stat` sites (permission-denied is the open question — implement as observation, confirm at review).
- [x] 1.4 `Edit`: return `ErrEditOldStringMissing` (empty/absent) and `ErrEditNotUnique` (multiple).
- [x] 1.5 `GrepRaw`: `ErrEmptyPattern` (empty) and `ErrInvalidRegex` (compile failure).
- [x] 1.6 `GlobInfo`: `ErrInvalidGlob` (`doublestar.Match` error).

## 2. Filesystem mechanism — middleware and wiring

- [x] 2.1 Create `internal/agent/middlewares/fs_error_middleware.go` mirroring `FSToggleMiddleware` (embed `TypedBaseChatModelAgentMiddleware`, gate on `fsToolNames`).
- [x] 2.2 Implement `WrapInvokableToolCall` + `WrapEnhancedInvokableToolCall`: after the endpoint runs, if `err != nil` and `errors.Is` matches the expected sentinel set, return a human-readable observation + `nil` error; if `context.Canceled`/`context.DeadlineExceeded`, return unchanged; otherwise return unchanged (fatal).
- [x] 2.3 Keep message discipline: name the requested path/value and reason; never reveal the absolute workspace root.
- [x] 2.4 Wire `FSErrorMiddleware` into `agent.go` `handlers` immediately after `fsToggle`.

## 3. Memory and knowledge-graph tools (direct conversion)

- [x] 3.1 `memory` tool (`internal/agent/tools/memory.go`): convert `coreStore.WriteCore` errors to observations — target-not-found, target-not-unique, unknown-op, and char-limit (include the "consolidate or remove old memories" guidance). Guard `ctx.Err()` first; keep genuine I/O errors fatal.
- [x] 3.2 `kg_search` (`internal/agent/tools/kg_search.go`): return the empty-`seed_entity_name` validation as an observation (`"<field> is required"`, `nil`) instead of `fmt.Errorf`.
- [x] 3.3 Leave `memory_search`/`session_search` as-is (they already return soft observations for the no-store/empty/no-results cases; only genuine DB errors are fatal, which is defensible).

## 4. Web tools (direct conversion)

- [x] 4.1 `web_fetch` (`internal/agent/tools/web/webfetch.go`): convert the terminal fetch failure (`return "", err` for both the preferred-provider-only path and the fallback-also-failed path) into an observation naming the URL and reason; guard `ctx.Err()` first.
- [x] 4.2 `web_search` (`internal/agent/tools/web/websearch.go`): same conversion for the terminal search failure; guard `ctx.Err()` first. Preserve the existing provider-fallback notice behavior.

## 5. Browser tools (direct conversion)

- [x] 5.1 Apply to every browser op (`navigate`, `act`, `screenshot`, `snapshot`, `console`, `tabs`, `open`, `close`, `start`, `stop`, `status`): convert `GetActivePage` and operation errors to observations naming the op and reason; guard `ctx.Err()` first.
- [x] 5.2 Specifically: no-active-page/engine-unavailable → observation so the agent can start a browser; navigation timeout, element-ref-not-found, and action/JS-eval failure → observations so the agent can re-snapshot/retry.
- [x] 5.3 Keep message discipline: do not leak internal CDP/SSRF resolution details.

## 6. Tests (all families)

- [x] 6.1 `tools_test`: each filesystem sentinel survives Eino-style `%w` wrapping and matches `errors.Is`.
- [x] 6.2 `tools_test`: `fsBackend` returns the correct sentinel for each expected condition.
- [x] 6.3 `middlewares_test`: `FSErrorMiddleware` converts expected sentinels to `(message, nil)`, leaves unknown errors fatal, and does NOT convert `context.Canceled`/`context.DeadlineExceeded`.
- [x] 6.4 `tools_test` (memory/kg): the `memory` tool declines and the `kg_search` empty-field case return observations (no fatal error).
- [x] 6.5 `web_test`: `web_fetch`/`web_search` terminal failures return observations; `ctx.Err()` is propagated.
- [x] 6.6 `browser_test`: no-active-page and a runtime failure (e.g. navigation/act error) return observations; `ctx.Err()` is propagated.
- [x] 6.7 Regression test: an expected failure across families (fs read `/tmp/...`, web unreachable URL, browser no-page) flows as an observation and does not surface as `event.Err` / a fatal stream error.
- [x] 6.8 Maintain black-box `_test` convention; keep `internal/agent/tools`, `internal/agent/tools/browser`, `internal/agent/tools/web`, and `internal/agent/middlewares` coverage ≥ 70%.

## 7. Verification

- [x] 7.1 `gofmt -s -w .` and `go vet ./internal/agent/... ./internal/web/... ./internal/browser/...`
- [x] 7.2 `go test ./internal/agent/... ./internal/web/... ./internal/browser/...`
- [x] 7.3 Behavioral check (CLI or chat): `read_file /tmp/...`, a `web_fetch` of an unreachable URL, and a browser call with no engine each continue the turn with an observation instead of stopping.
- [x] 7.4 Run `openspec verify --change harden-tool-error-signaling` before archive.
