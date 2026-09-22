## Why

Connection tools bypass the workspace tools allowlist and reach every user who can talk to an attached agent: today a Member can ask Atlas to merge a PR or delete a Confluence page, because the only enforcement is the token's own scopes. The token bounds what the *service* permits; nothing bounds what this *user* may ask for through the agent. This is the confused-deputy hole in the connections architecture, and the fix is the user-authority inner wall: verbs tiered per service, gated by the requesting user's permissions at the tool chokepoint.

## What Changes

- **Recipes declare verb tiers**: every tool a recipe's connection exposes is classified `read` or `write` in recipe data (undeclared tools default to `write` — fail-safe).
- **A connection tool gate at the chokepoint**: during tool resolution and invocation for connection-sourced tools (MCP-kind and HTTP-kind alike), the gate checks the requesting user's permissions — `read`-tier tools ride workspace membership; `write`-tier tools require `integrations.write`. The gate is transport-agnostic: it keys off the connection, not the transport.
- **Deny = canonical block**: a blocked call returns the canonical block result as the tool outcome — soft, visible in the transcript, never a run failure — matching the hooks philosophy.
- **Service-authority runs are read-tiered**: webhook-triggered runs (no requesting user) may use `read`-tier tools; a `write`-tier need escalates through the approval flow — the turn pauses, an Owner/Admin approves or denies from the transcript, and the run resumes.
- **Visible tiers**: the agent config integrations section shows each attached connection's tool count per tier, so admins see what Members can and cannot drive before it matters.

## Capabilities

### New Capabilities

- `integration-authority`: tiered verb gating for connection tools — recipe-declared tiers, the transport-agnostic chokepoint gate, member/read vs admin/write mapping, canonical-block denies, service-authority tiering with approval escalation.

### Modified Capabilities

<!-- None: the gate is a new seam beside existing tool resolution; recipes gain declaration data; no existing requirement changes behavior for PAT-era flows that already require integrations.write to manage connections. -->

## Impact

- **Domain** (`internal/domain`): tier values on recipe tool declarations.
- **Runner** (`internal/agents`): the gate consults at connection-tool resolution and invocation; approval escalation reuses the existing interrupt/resume machinery.
- **Connections service**: tier lookup from recipe data; effective-tier projection for agent config.
- **Web**: tier counts in the agent config integrations section; approval affordance reuses the existing shell-approval transcript pattern.
- **Smoke tests**: member-requested write denial, admin allowance, webhook-run escalation.
