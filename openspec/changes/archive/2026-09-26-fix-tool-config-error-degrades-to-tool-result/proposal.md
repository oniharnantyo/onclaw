# Proposal

## Why

An agent that exposes `web.search` in a workspace with no configured search provider cannot run at all: the tool's "not configured" constructor error fails tool resolution before the model is ever called, and the user sees only "Run failed". This contradicts the runtime's own declared principle (the tool-error-result middleware: "a failing tool must not kill the run") — a configuration gap is turn data the agent should reason over and relay, not a system failure. It also makes every other latent construction-time failure mode one refactor away from the same run-killer.

## What Changes

- `web.search`'s provider resolution moves from tool construction to first invocation (lazy, memoized per execution): the tool always builds, and the not-configured / unknown-provider / missing-credential errors surface at invocation as error results the model reads — on the tool call card in the transcript — while the run completes and the agent answers another way (own knowledge, `web.fetch`).
- The error text stays explicit and actionable ("web.search is not configured — add a provider in Settings → Tools"); the error is still raised before any network attempt, and there is still no credential-free fallback.
- The stale zero-credential DuckDuckGo fallback language in the `agent-runtime` spec is corrected to match the removed-backend reality pinned in `workspace-tools`.
- File-family tool constructors (`files.delete`, `document.read`, `document.create`) self-heal a missing agent workspace directory (`MkdirAll` at construction, mirroring the system-skills "disk is a self-healing cache" stance) instead of failing the run on `stat`; only a genuinely unwritable disk still errors.
- Channel/session tool constructors keep their defensive construction guards — they are unreachable (exposure is stripped at resolution) and their nil-checks are on optional run context.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `workspace-tools`: "Search provider configuration" — when neither workspace entries nor a usable instance-env provider exist (or a named provider is unknown / lacks its credential), `web.search` no longer fails construction: it builds and every invocation returns an explicit error result naming the missing configuration, surfaced on the tool call while the run proceeds. "Fails fast before any network" and "no credential-free fallback" are preserved.
- `agent-runtime`: "Web search tool providers" — same degradation semantics for the misconfigured-provider case, and the stale zero-credential DuckDuckGo fallback scenario is replaced with the current no-credential-free-backend behavior.

## Impact

- `internal/agents/tools/websearch.go` — lazy provider resolution (resolver closure, per-execution memoization), updated package doc; `NewWebSearch` keeps its schema; new lazy-provider option.
- `internal/agents/tool_registry.go` — `web.search` registration becomes infallible; `searchProviderFor` moves behind the resolver closure; construction-error comment updated.
- `internal/agents/tools/delete_file.go`, `document_read.go`, `document_create.go` — self-heal the agent dir instead of stat-failing.
- Tests: `tool_registry_test.go` (construction-error expectation flips to invocation error), `websearch_test.go`, file-tool ctor tests.
- No API, migration, or frontend changes — the transcript already renders error tool-call cards.
