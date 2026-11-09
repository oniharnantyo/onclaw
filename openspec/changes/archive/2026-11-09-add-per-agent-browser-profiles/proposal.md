# Proposal

## Why

Every agent currently shares one browser profile (`default`): the same cookie jar, and worse, the same live Chromium session — two agents using browser tools concurrently navigate over each other's tab and see each other's logins. Profile data also lives under `~/.buddies/browser/profiles/`, disconnected from agent lifecycle: deleting an agent leaves its logins behind, and nothing ties an identity to the agent that earned it.

## What Changes

- Each agent gets its **own browser profile**, stored **inside the agent's workspace directory** (`~/.buddies/workspaces/<slug>/browser-profile/`), so logins live and die with the agent — deleting an agent's workspace removes its browser identity with zero extra cleanup.
- The binding is **host-enforced**: the parent stamps the agent's profile directory through the existing per-call `_meta` channel (the same seam that already routes screenshots into the workspace jail). When `_meta` binds a profile directory, the server uses it and the model-supplied `profile` argument is ignored — the model neither sees nor chooses profiles.
- Agents with their own profile are immediately takeover-capable: the bound directory is created on demand, so `request_takeover` no longer refuses a not-yet-used profile and a person can sign in once per agent; the cookie jar persists it.
- Direct CLI use of `browser-mcp` (no `_meta`) keeps today's behavior exactly: named profiles under `~/.buddies/browser/profiles/<name>/`, the configured default profile, the `default` fallback. No migration of existing logins.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `browser-use`: the **Persistent browser profiles** requirement changes — a call whose `_meta` binds a profile directory (host-stamped per calling agent) resolves to that directory instead of a named profile under the browser root; the `profile` argument is ignored on such calls. A new **Agent profile binding** requirement pins the parent-side contract: the parent stamps the calling agent's workspace profile directory and name, binding is per agent (every agent its own profile), and takeover/secret/cookie tools honor the binding.

## Impact

- `internal/browseruse`: profile resolution gains a `_meta`-bound path (new meta keys alongside `MetaScreenshotDir`); session keying extends to bound directories; `profileExists` auto-creates bound dirs; cookie jar, takeover relaunch, and idle reaper work unchanged on the bound dir.
- `internal/workspaces`: new agent-level `browser-profile/` directory helper alongside `skills/` and `.tasks/`.
- `internal/agents/mcp` + `internal/agents`: the browser tool invocation site stamps the calling agent's profile dir/name into `_meta`; attention-channel takeover invocations carry the same binding.
- No frontend, schema, or settings changes. `~/.buddies/browser/profiles/` and the default-profile setting remain for direct CLI use; existing logins there are untouched.
- Cost note: each busy agent profile is its own Chromium process (~150–300 MB); idle browsers are still reaped after the idle timeout.
