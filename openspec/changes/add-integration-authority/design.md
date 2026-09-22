## Context

Connection tools deliberately bypass the workspace tools allowlist (change add-workspace-connections' architecture: MCP tools ride the MCP policy; HTTP request tools ride the connection tool source). Token scopes bound what the service permits, and `integrations.write` bounds who may *manage* connections — but any Member can *drive* a connected service through an attached agent at full token strength. The earlier explore established the two-walls model; the outer wall (tokens, access levels) shipped in the connections change. This is the inner wall.

## Goals / Non-Goals

**Goals:**
- One gate, every transport: MCP tools, request tools, and future connection kinds tier identically by connection.
- Denies that teach: canonical block results naming the missing permission, runs that continue.
- Service runs that cannot silently write on the team's behalf.

**Non-Goals:**
- Per-tool permission management UI (tiers are recipe data; workspaces get two effective levels).
- A third "comment" tier (collapsed into read for v1 — commenting is not destructive; revisit on demand).
- Gating built-in tools (files, web, memory) — the existing tools allowlist and workspace policy already own those.
- Audit log surfaces beyond what traces already record (attribution lands with each trace row).

## Decisions

- **D1 — Gate at the connection-tool chokepoint, transport-agnostic.** Both tool sources (MCP, HTTP request tools) yield tools wrapped with their connection identity; the gate consults at resolution *and* invocation (resolution prunes for budget clarity; invocation re-checks against the persisted permission set in case roles changed mid-run). Alternatives rejected: per-transport gates (duplicated logic, drift risk) and gating inside hooks (hooks are workspace/agent policy — user authority is platform semantics, not configurable policy).
- **D2 — Two tiers mapped to existing permissions.** `read` → membership; `write` → `integrations.write`. No new permission, no catalog change: the connections change already established `integrations.write` as the admin-tier integration permission, and Member's read-only set cannot hold it. Fail-safe default: undeclared = write.
- **D3 — Canonical block, not run failure.** A denied call returns the hooks-style block result (`{"decision":"block","reason":...}` shaped tool outcome) naming the required permission, mirroring the hooks philosophy that policy is soft infrastructure — visible in the transcript, never a crashed run.
- **D4 — Service runs read-tiered with interrupt/resume escalation.** Webhook runs carry service authority (connection-webhooks change); at the first write-tier call the run interrupts using the existing shell-approval machinery, rendered as an approval card in the bound thread. Approver set: any member holding `integrations.write`. Denial ends the turn with the denial recorded in the transcript and trace.
- **D5 — Tiers ride recipe releases.** Tier declarations ship with recipes (like endpoints and scopes), so upstream tool renames are caught by the same live-verification cadence; a tool the recipe does not know defaults to write-tier — new provider tools never silently become member-accessible.
- **D6 — Resolution pruning + invocation re-check.** Pruning write-tier tools for non-admin users also saves tool-budget context; the invocation-time re-check covers role changes mid-run and the resume path.

## Risks / Trade-offs

- [Recipe tier misclassification under-blocks or over-blocks] → fail-safe default (undeclared = write); live verification reviews tier lists per release; over-blocking is visible immediately (members complain), under-blocking surfaces in traces.
- [Approval friction on service runs] → read-tier default event sets keep most webhook runs approval-free; write escalation is opt-in per run, and teams can attach a dedicated agent whose connection tokens are read-scoped (belt and suspenders).
- [Mid-run role changes] → invocation re-check reads the current permission set; a demoted member's in-flight write call is denied at invocation even if resolution pruned it in.

## Migration Plan

1. No schema change: tiers are recipe data; the gate ships inert for tools already restricted (write-tier default preserves current admin-only behavior for anything unclassified).
2. Rollback: remove the gate wrapper; behavior returns to token-scope-only enforcement with no data residue.

## Open Questions

None blocking. Per-service tier lists (which GitHub MCP tools are read vs write) are recipe data pinned during apply alongside the existing live-verification tasks.
