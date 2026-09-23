## Context

`internal/agents/mcp/client.go` launches stdio servers via mcp-go's `client.NewStdioMCPClient(command, env, args...)`. The current `envSlice` helper flattens configured `EnvRow`s into `KEY=VALUE` strings, and mcp-go **merges them over the parent environment** (the doc comment even states "configured vars win without dropping PATH"). Consequence: every stdio child inherits the OnClaw server process's full environment — including `DATABASE_URL`, `ONCLAW_JWT_SECRET`, hook secret material, and storage credentials. All dials (run-path via the manager, probe-path via `Probe`) go through the same `dial` function, so there is exactly one place to fix.

Peer ground truth: Hermes explicitly passes only declared env plus a safe baseline, documenting the rationale as secret-leakage containment.

## Goals / Non-Goals

**Goals:**
- Child env = safe baseline ∪ configured rows; configured rows win on conflicts.
- One construction path shared by probe and run dials (they already share `dial`).
- Cheap to verify: the existing mock stdio server echoes its env in tests.

**Non-Goals:**
- Redaction/allowlisting of what goes *into* configured rows (that's the existing secret-rows machinery).
- Changes to HTTP/SSE transports, header handling, or the Windows-specific env semantics (deployment target is POSIX).
- Sandboxing the child beyond environment (no filesystem/network isolation) — that's a different, much larger change.

## Decisions

- **D1: Fixed baseline constant, not configuration.** The baseline is a package-level allowlist (`PATH`, `HOME`, `TMPDIR`, `LANG`, `LC_ALL`, `TZ`, `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, lowercase forms), copied from the parent only if present. Rationale: a configurable baseline reintroduces the leak via misconfiguration; the spec pins the minimum set, the constant implements it. Alternative considered — drop-in `env=[]` (empty child env): rejected; servers that shell out to `git`, `node`, `docker` need PATH/HOME, and proxy vars are required in enterprise networks (LinkAja-style deployments sit behind proxies).
- **D2: Construct via `os environ filter`, not mcp-go options.** Build the child env as a `[]string` of `KEY=VALUE` (baseline ∪ rows, rows appended last so they win) and pass it to `NewStdioMCPClient` as today — mcp-go replaces the child env wholesale when a non-nil env is supplied? **No:** verified mcp-go merges a supplied env over the parent. So the merge must not be relied on: the constructor receives ONLY the constructed env and the launch path must opt out of mcp-go's parent-merge behavior. Mechanism: set `cmd.Env` explicitly. If mcp-go's stdio transport unconditionally inherits (`cmd.Env == nil` → inherit; non-nil → replace), passing the constructed slice is sufficient — verify against the pinned mcp-go version and pin the behavior with a regression test (mock server echoes its env; assert no `DATABASE_URL`). This verification is task 1 and gates everything else.
- **D3: No status/surface changes.** The connection status model (connected/error) already captures dial failures; a server that breaks because it lost an inherited var shows up as a normal probe error with the server's own message. No schema, API, or UI work.

## Risks / Trade-offs

- [A deployed stdio server silently depended on an inherited variable] → BREAKING note in the change log; the probe surfaces the failure immediately with the server's error, and the operator fix is one env row on the MCP server config.
- [mcp-go changes merge semantics across versions] → regression test asserts the constructed-env contract against the pinned version; go.mod bump re-runs it.
- [Proxy-dependent sites lose connectivity if we forget a proxy variable] → baseline includes upper+lowercase proxy forms; NO_PROXY covers CIDR/wildcard exclusions.

## Migration Plan

Ship as a normal backend change; no migration. Rollback = revert (behavior is contained in one function). Release notes call out the breaking semantics for stdio servers that relied on inherited variables.

## Open Questions

- None. D2's mcp-go behavior check is a task, not a decision — the constructed-env contract holds either way (worst case we pre-filter the parent env ourselves before handing it to mcp-go).
