# Design

## Context

Agents store a tool allowlist (`agents.tools text[]`, migration 000017) resolved at run start in the runner's `resolve()`: allowlist → per-turn `AllowedTools` override → channel/session scoping → workspace gate → context strips → registry expansion (browser facade, reserved names) → MCP + connection tools appended. The denylist design already existed once (migration 000016's `disabled_tools`) and 000017 replaced it. Skills "enable everywhere" (`internal/skills/service.go`) unions required tool names into every agent's allowlist; v1 request narrowing (`internal/server/handlers/v1.go`) intersects request tool names with the allowlist; the API already treats removed fields as accepted-but-ignored (managed-field convention for `disabled_mcps`, `disabled_skills`). See proposal.md for motivation and the spec deltas for the behavior contract.

## Goals / Non-Goals

- Goals: denylist storage and resolution; new catalog tools appear for every agent automatically; no data backfill; skills force-enable by subtraction; UI default-selected chips.
- Non-Goals: `enabled_mcps` stays an allowlist (workspace MCP opt-in is deliberate and out of scope); no per-tool `DefaultOff` catalog flag in this change (reserved capability names become default-on — a follow-up can add the marker without schema change); no bulk denylist editing across agents.

## Decisions

- **D1 — Field `disabled_tools`, denylist semantics, empty exposes everything.** Reuses migration 000016's original column name. The down migration restores an empty `tools` column (config data loss on down — the same convention as 000017's own down).
- **D2 — Resolution order preserved, the allowlist stage inverts.** effective = catalog − `disabled_tools`; when the per-turn `AllowedTools` override is provided it still replaces resolution for the turn (it is request-scoped *narrowing* — an explicit allowlist is its natural shape, and v1 + eval fixtures depend on it). Channel/session scoping inverts to un-scoping: context tools are pulled out of the disabled set on those runs. The workspace gate still wins over everything. Alternative considered: model the per-turn override as a per-turn denylist — rejected, it would change v1 request semantics.
- **D3 — Reserved names flip to opt-out.** `execute` (shell), `browser` (facade — disables the whole browser set), `subagents`, `background_shell` are enabled unless named in `disabled_tools`; individual `browser.*` names disable individually. The in-flight `add-agent-subagents-background` change pins these as opt-in — its delta specs must adopt opt-out; denylist semantics govern when both land.
- **D4 — No backfill; migration 000070 drops `tools`.** `ADD COLUMN disabled_tools text[] NOT NULL DEFAULT '{}'`, `DROP COLUMN tools`. Every existing agent starts with everything exposed — the user's explicit choice; stale allowlist data is discarded. Requests still carrying `tools` are accepted and ignored (managed-field convention), so writers don't break; responses no longer carry `tools` (BREAKING, flagged in the proposal).
- **D5 — Skills force-enable by subtraction.** "Enable everywhere" adds required tools to the workspace gate and removes them from every agent's `disabled_tools` in one transaction, replacing `hasAllTools`/`mergeTools`; unmet-dependency checks read the effective set.
- **D6 — UI inverts.** Step 3 chips render selected unless their key is in `disabled_tools`; new catalog tools appear selected automatically; workspace-disabled chips stay greyed and unselectable; the Browser and Shell chips write the alias / `execute` to the denylist on deselection.

## Risks / Trade-offs

- [Existing agents gain every tool at migration — including shell, delete_file, subagents, background_shell] → deliberate (no backfill). The workspace tool gate is unchanged and remains the policy layer; workspace admins can disable tools workspace-wide, and individual agents can be denylisted after migration.
- [Least-privilege posture flips to default-open at the agent tier] → agent-level becomes preference, workspace-level stays policy; the gate ordering is unchanged.
- [Readers of the API lose the `tools` field] → breaking, flagged; writers sending `tools` get the accepted-ignored treatment the API already uses for removed fields.
- [Coordination with the uncommitted `add-agent-subagents-background` change] → noted in the proposal; its reserved-name opt-in must adopt opt-out.

## Migration Plan

1. Migration 000070 (add `disabled_tools` default `'{}'`, drop `tools`; down = exact inverse).
2. Backend: domain field; store scan/args (fake + postgres); create/patch handlers (nil → `{}`, replace semantics, `tools` ignored); runner resolution and todo-exposure check; fs-middleware disable computation; skills subtraction; v1 narrowing; eval seed fixtures.
3. Web: types, `AgentConfigModal` Step 3 toggle logic, unmet-dependency check, API payloads.
4. Smoke suite updates.

Rollback: migration down restores an empty `tools` column (tool-config history is not recovered — accepted).

## Open Questions

- None blocking. If default-on `subagents`/`background_shell` proves too permissive in practice, a follow-up can add a `DefaultOff` catalog marker without touching the denylist schema.
