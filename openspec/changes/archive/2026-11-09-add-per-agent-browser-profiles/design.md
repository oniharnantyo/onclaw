# Design

## Context

Browser Use is one supervised system MCP server (design D2 of add-browser-use-rebrand; `internal/agents/mcp/systemserver.go`) shared by every agent. Profiles resolve by name under `~/.buddies/browser/profiles/<name>/` (`internal/browseruse/profile.go`); every agent's calls land in the same `default` profile and therefore the same live Chromium. The parent already stamps per-call `_meta` on system-server tool invocations: `sessionDirTool` (`internal/agents/mcp.go`) wraps every listed tool and carries `buddies.dev/screenshot-dir` through `MCPManager.Invoke` into the child, and the child reads `_meta` in its handlers (the screenshot routing). Agent workspaces (`internal/workspaces`) are one directory per agent slug at `~/.buddies/workspaces/<slug>/`, holding agent-level `skills/` and `.tasks/` and per-session `sessions/<sid>/…`; `Manager.Remove(slug)` deletes the whole tree.

See proposal.md for motivation; the delta spec pins the observable contract.

## Goals / Non-Goals

**Goals:**
- Every agent's browser tool calls resolve to that agent's own profile directory, stored inside the agent's workspace.
- Binding enforced parent-side; the model can neither see nor influence it.
- Takeover, secret, and cookie tools honor the binding; a fresh agent is takeover-capable on first use.
- Direct CLI use of `browser-mcp` keeps today's named-profile behavior byte-for-byte.

**Non-Goals:**
- No migration of existing `~/.buddies/browser/profiles/` logins into agents (a later "import into agent" action can build on `ImportProfile`).
- No per-agent spawn of separate `browser-mcp` processes, no per-agent settings (headless, timeouts stay global tool settings).
- No frontend changes; no schema migration; no changes to third-party MCP servers' meta contract.

## Decisions

**D1 — Binding rides the existing `_meta` channel, not a registry or per-agent servers.**
Two meta keys join `MetaScreenshotDir`: `buddies.dev/profile-dir` (absolute profile directory) and `buddies.dev/profile-name` (display name, e.g. the agent slug). Alternatives rejected: a name→dir registry file under the browser root (second source of truth, stale entries on agent delete, server must re-read or watch it) and one `browser-mcp` child per agent (new dynamic supervisor plumbing — the system supervisor registry is built at manager init — plus N children and N locks, against the single-server design). The `_meta` channel already has exactly the right trust and lifecycle properties: parent-stamped, per-call, model-blind, dies with the call.

**D2 — Bound directory lives at `<workspace>/<slug>/browser-profile/` (agent level, like `skills/`).**
`workspaces` gains a `BrowserProfileDir(slug)` helper (mkdir -p on demand, `dirPerm`). Agent deletion needs zero new cleanup: `Manager.Remove` already removes the tree. The dir is distinct from the existing session-level `browser/` screenshots dir — screenshots are session artifacts, the profile is agent identity. Alternative rejected: symlink from `~/.buddies/browser/profiles/<agent>` into the workspace (symlink privileges on Windows; the platform-agnostic rule favors plain directories).

**D3 — Server resolution: bound meta wins; named profiles stay the fallback.**
In `internal/browseruse`, every handler's profile resolution funnels through one seam (today `b.profile(args.Profile)` → `ResolveProfile` → `ProfileDir`). That seam gains: when the call's `_meta` carries `profile-dir`, validate it is a clean absolute path, `MkdirAll` it, use it as the user-data dir and as the sessions-map key (a string — no map-type change; bound dirs cannot collide with names since bound keys are absolute paths), and ignore the `profile` argument entirely (no `ValidateBrowserProfile` on it). `profile-name` is display-only (echoed in results such as the takeover receipt; any value accepted). Without the meta, resolution is exactly today's path — `profileExists` keeps refusing unknown names, so takeover-by-name cannot reach an agent's bound dir behind the parent's back: takeover requests from an agent arrive through the parent's channel tools, which carry the binding themselves.

**D4 — The takeover/secret channel invocations carry the binding.**
`attention.go` reaches the child's reserved tools (`__request_takeover`, `__begin_takeover`, `__resolve_attention`, `__request_secret`, `__deliver_secret`) via `invokeChildTool(ctx, ref, name, argsJSON)`, which today sends no `_meta`. It gains the binding (the wrapper sites know the agent's workspace). Consequence: an agent's takeover parks, relaunches headed, and persists sign-in state on the agent's own dir; `profileExists` passes because D3 auto-creates bound dirs. The attention card prompt names the agent ("hand over the browser of agent `<name>`") instead of a profile literal.

**D5 — Stamping site is `sessionDirTool`, gated to the browser-use server.**
`sessionDirTool` already wraps every system-server tool per session and knows the workspace. It gains `profileDir`/`profileName` fields (resolved at listing time alongside `dir`), stamped only when `ref.ServerID` is the browser-use system server — computer-use ignores the extra keys today, but explicit gating keeps the meta contract per-server. The model-facing tool schemas are unchanged: `profile` stays a valid argument for unbound callers; for bound calls the server ignores it (no per-agent schema surgery in the composite surface).

## Risks / Trade-offs

- [Each busy agent runs its own Chromium] → Idle reaper (120 s default) closes idle browsers regardless of profile; only concurrently-active agents cost memory (~150–300 MB each). Accept: it is the price of isolation the user chose.
- [Bound dir trust: `_meta` chooses an arbitrary absolute path] → The meta contract is already parent-only (stdio child of the parent; third parties cannot reach it), and the screenshot key set the same precedent. Server still validates absoluteness and creates with `0700`; no name-contract bypass is possible from model-side arguments.
- [Agent slug as display name can exceed the 32-char profile contract] → Irrelevant by construction: bound names never touch `ValidateBrowserProfile` (D3); only unbound (named) profiles are contract-checked.
- [Two agents sharing one account site → the site sees two sessions] → Correct per isolation requirements; nothing to mitigate.
- [Existing `default` logins orphaned for agents] → Accepted by user (all-agents rollout). Follow-up option: an "import into agent" action over `ImportProfile`.

## Migration Plan

Ship-and-forget: no data moves, no settings change, no schema bump. Existing agents start with empty (logged-out) profiles on first browser use; `~/.buddies/browser/profiles/` remains for direct CLI use. Rollback = revert; bound dirs under workspaces are inert data.

## Open Questions

None — the two user-locked decisions (profile inside the agent dir; all agents get their own) plus the storage-mechanism choice above close the forks this design raised.
