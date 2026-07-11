## Context

Today the agent create flow (`web/src/pages/AgentDetailPage.tsx`, `mode === 'create'`) renders two
tabs — Overview and Tools — and seeds the form from `DEFAULT_FORM` whose `tools` value is the literal
`'shell'` (`AgentDetailPage.tsx:34`). On save, `saveAgent` POSTs that value, so a UI-created agent
row stores `tools = 'shell'`.

Meanwhile every other path that creates an agent produces an **empty** allowlist:

- `onclaw agent add` has no `--tools` flag and builds `store.Agent{}` without setting `Tools`
  (`internal/cli/agent_cmd.go`), leaving the Go zero value `""`.
- The `agents.tools` column default is `''` (`internal/store/sqlite/db.go`).
- At assembly, `internal/agent/agent.go:198-215` only applies the per-agent allowlist when
  `agentConf.Tools != ""`; an empty string means **no filtering**, so every globally-enabled builtin
  tool is attached.

So an empty allowlist already means "all builtin tools" everywhere except the web create form. The
web edit experience also mis-reads this: the per-agent Tools tab's `isToolEnabled` returns `false`
for every tool when `agentTools` is empty (`Tools.tsx:99-106`), so an agent that *runs* with all
tools *displays* as having none.

`web-ui` already requires that web field semantics match the `onclaw agent` subcommands. This change
closes that gap and aligns the display.

## Goals / Non-Goals

**Goals:**

- Web-created agents get the same default tool set as `onclaw agent add` (all globally-enabled
  builtin tools), with no Tools section on the create form.
- The per-agent Tools tab truthfully represents an empty allowlist as "all enabled."
- No store, schema, API-route, CLI, or assembly changes; one contained backend edit to
  `Service.SetAgentTools` so toggling from an empty allowlist persists (see Decision 4).

**Non-Goals:**

- Redirecting create-success to the edit page (stays on the list, as today).
- Create-time authoring of hooks, skills, MCP, memory, or persona — these remain edit-time because
  they are scoped to a persisted agent row (hooks/skills/MCP via soft `scope` binding, memory as a
  column on the row, persona as workspace files resolved through `GetAgent`). See the explore notes.
- Migrating existing web-created agents off `'shell'`. Agents already stored with `'shell'` keep
  their explicit allowlist; changing them silently would alter existing behavior without consent.
- Introducing a wildcard/`*` token. Empty string is the established "all" sentinel.

## Decisions

### Decision 1 — Represent "all tools" as an empty allowlist (Shape A), not an explicit list

The create form sends `tools: ''` (empty). Assembly already attaches all globally-enabled builtin
tools for an empty allowlist, so the runtime behavior is correct with **zero backend change**.

**Alternatives considered:**

- **Shape B — backend expands empty → explicit list of every builtin name on create.** Rejected. It
  is redundant work: assembly already implements empty=all, so storing an explicit list diverges
  from the in-memory semantic. It also *freezes* the set: a builtin tool added next month would not
  be enabled for agents created today, contradicting "default all builtin tools" as a living
  default. It further bloats each agent row with 18+ comma-separated names.
- **Shape C — frontend fetches `/api/tools` on create and sends the joined name list.** Rejected for
  the same freezing reason, plus it adds a network fetch to a page that no longer renders tools.

Shape A makes "all builtin tools" dynamic — newly registered tools automatically apply to every
agent that has no explicit allowlist — which matches how the CLI and the builtin master agent
already behave.

### Decision 2 — Fix the per-agent Tools tab display as a required correctness fix

`Tools.tsx` `isToolEnabled` and `toggleTool` are adjusted so an empty `agentTools` is treated as
"all on" (matching assembly), and disabling one tool from the all-state computes the explicit list
of all *other* registry names (sourced from the already-loaded `categories`). This is not optional:
without it, opening a freshly-created agent's Tools tab would show every tool disabled while the
agent actually runs with all of them — a visible lie.

### Decision 3 — Remove the Tools tab from create outright (not disable it)

Create becomes Overview-only; the tab bar does not render (`tabsConfig.length === 1`). Keeping a
disabled/hidden Tools tab would be dead UI. The `Tools` component's existing `agentName ===
undefined` ("create mode → local state only") branch becomes unreachable from the create page and is
left in place (harmless, still used if the component is ever re-employed for create).

### Decision 4 — One small backend edit is required for correct toggle-from-all

The display and create defaults are frontend-only (`AgentDetailPage.tsx`, `Tools.tsx`), and the
empty-allowlist = all-builtin-tools semantic already exists across CLI, assembly, and schema. But
the web edit Tools tab toggles through `PUT /api/agents/{name}/tools`, which hits
`Service.SetAgentTools`. Its per-tool branch only handled a **non-empty** allowlist: when
`agent.Tools == ""` (all) and the client disables one tool, the split yields `[]`, so the DB was
rewritten to `""` again — the disable was silently dropped and "all" was re-asserted on every
reload. That broke the spec scenario "disable one → only it turns off and the agent no longer
offers it on the next run."

So one backend edit is required: in `SetAgentTools`, when `agent.Tools == ""`, treat the state as
"all registry tools" symmetrically — disabling a tool stores every other registry tool name
(derived from `toolRegistryStore.ListTools`, exactly as the existing `tool == "*"` branch already
does), and enabling a tool is a no-op (stays empty). This is a contained, additive change to one
function; no schema, store, API route, CLI, or assembly changes.

## Risks / Trade-offs

- **Existing web agents keep `'shell'`.** → Acceptable and correct: we do not rewrite existing
  allowlists. Users who want all tools on an older UI-created agent edit it once. Documented as a
  non-goal.
- **Toggle-from-all edge case.** Disabling a single tool when the allowlist is empty must yield
  "all except that tool," not the empty set. → Mitigation: derive the full name set from the loaded
  `categories` and subtract; cover with a unit/component test.
- **Empty allowlist ambiguity for "deliberately none."** A user who wants zero tools cannot express
  it (empty = all). → Accepted trade-off; this was already true at runtime, and "an agent with no
  tools" is a negligible use case. Globally disabling tools via `tool_registry` remains available.
- **Feature-gate interaction.** Even with empty=all, memory/kg tools are still withheld when their
  feature flags are off, and any globally-disabled tool is withheld. → Not a risk; "all" means
  "all applicable," consistent with today.

## Migration Plan

Frontend deploy = ship the UI build; backend deploy = ship the Go binary. Rollback = revert the two
frontend files (`web/src/pages/AgentDetailPage.tsx`, `web/src/components/Tools.tsx`) and
`internal/api/service/agent.go`; existing agents are unaffected because their stored allowlists are
unchanged.

## Open Questions

None material. (An optional, explicitly out-of-scope follow-up: land create-success on
`/agents/:newName` instead of the list, so post-create refinement is one click — left for a separate
change.)
