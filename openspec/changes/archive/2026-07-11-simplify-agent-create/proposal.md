## Why

The web **Add Agent** flow diverges from the CLI and from the agent-assembly semantics in a way
that surprises users and violates an existing `web-ui` requirement. The create form hardcodes
`tools: 'shell'` (`web/src/pages/AgentDetailPage.tsx:34`), so an agent created through the UI gets
**only** the `shell` tool. But `onclaw agent add` sets *no* `--tools` flag and builds the row with an
empty `Tools` field (`internal/cli/agent_cmd.go`), which assembly treats as **all builtin tools**
(`internal/agent/agent.go:198-215` skips filtering when the allowlist is empty). The DB column
default is likewise `''`. So CLI-created agents get every builtin tool; UI-created agents get one.
`web-ui` already mandates that "Field semantics SHALL match the `onclaw agent` subcommands" — this is
a conformance gap.

The create page also surfaces a Tools tab that is rarely needed at creation time: sensible defaults
should make tool selection a post-create refinement, not a required create step. Removing it makes
create a focused identity+model form.

Finally there is a latent display bug: the per-agent Tools tab renders an **empty** allowlist as
"all tools off" (`web/src/components/Tools.tsx:99-106`), contradicting assembly's "empty = all."
Aligning the display is a required correctness fix that falls out of this change.

## What Changes

- **Remove the Tools tab from the create page** (`/agents/new`). Create becomes an Overview-only
  form: name, provider, model (+ metadata), reasoning, system prompt, workspace, max-iterations,
  default flag. No tab bar renders.
- **Web create sends an empty `tools` value** instead of `'shell'`, matching `onclaw agent add`.
  A newly created UI agent therefore receives **all globally-enabled builtin tools** by default —
  the same set the CLI produces.
- **Fix the per-agent Tools tab display** so an empty allowlist reads as "all enabled" (consistent
  with assembly), and toggling a tool off from the all-state computes the correct explicit
  allowlist (all-names minus the disabled tool) rather than no-oping.
- **Fix `SetAgentTools`** (`internal/api/service/agent.go`) so toggling a single tool from an empty
  allowlist persists: disabling one tool stores every other registry tool, and enabling is a
  no-op. Without this, a disable-from-all was silently dropped and "all" re-asserted on reload.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `web-ui`: the agent create form is a focused identity+model form with no Tools (or other
  resource) section, and a new agent's tool set defaults to all builtin tools — matching
  `onclaw agent add` and the assembly's empty-allowlist semantic.
- `tools-management`: codify that an agent with an empty per-agent allowlist is offered **all**
  globally-enabled builtin tools (empty = all, not none), resolving the existing ambiguity in the
  "intersection" requirement and bringing the spec in line with the load-bearing assembly behavior.

## Impact

- **Frontend edits** in `web/src/pages/AgentDetailPage.tsx` (remove the create-only Tools tab; change
  the create default `tools` from `'shell'` to empty) and `web/src/components/Tools.tsx` (treat empty
  `agentTools` as all-enabled in `isToolEnabled`, and fix `toggleTool` for the all→minus-one case).
- **One backend edit** in `internal/api/service/agent.go` `SetAgentTools`: the per-tool branch now
  handles an empty allowlist symmetrically (disabling one tool of "all" stores every other registry
  tool; enabling is a no-op). Without it, a disable-from-all was silently dropped. No schema, store,
  API route, CLI, or assembly changes.
- **No migrations, no new dependencies.**
- **Out of scope**: redirecting create-success to the agent edit page, and any create-time
  authoring of hooks/skills/MCP/memory/persona (those remain edit-time, scoped to an existing
  agent row — see `design.md`).