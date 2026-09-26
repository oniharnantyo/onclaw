# integration-authority Specification

## Purpose

Closes the confused-deputy hole in connections: service power reachable through an attached agent is bounded by the requesting user's authority, not just the token's scopes. Verbs are tiered per service in recipe data, and a transport-agnostic gate at the connection-tool chokepoint maps read-tier verbs to workspace membership and write-tier verbs to `integrations.write`.

## Requirements

### Requirement: Verb tier declarations

Recipes SHALL declare a tier (`read` or `write`) for every tool their connections expose. Tools without a declared tier SHALL be treated as `write` — fail-safe. Tier declarations are recipe data, updatable in releases without connection re-configuration.

#### Scenario: Undeclared tool defaults to write

- **WHEN** a recipe ships a new tool without a tier declaration
- **THEN** the gate treats it as write-tier until a release declares otherwise

### Requirement: The connection tool gate

Tool resolution and invocation for connection-sourced tools — MCP-kind and HTTP-kind alike — SHALL consult a gate keyed off the originating connection and its recipe tiers: `read`-tier tools SHALL require only workspace membership, and `write`-tier tools SHALL require `integrations.write` in the requesting user's permission set. The gate SHALL be transport-agnostic: it applies by connection, identically to MCP servers and request tools. Built-in non-connection tools are out of its scope.

#### Scenario: Member uses read-tier tools

- **WHEN** a Member asks an attached agent for open issues through a connected service
- **THEN** the read-tier call resolves and executes normally

#### Scenario: Member blocked from write-tier

- **WHEN** a Member asks the agent to merge a PR through a connected service
- **THEN** the call is blocked before any network request, and the tool result is the canonical block outcome naming the required permission — the run continues, not fails

#### Scenario: Admin passes the write tier

- **WHEN** a user holding `integrations.write` requests the same write-tier action
- **THEN** the call executes subject to the token's own scopes

### Requirement: Service-authority tiering

Runs triggered without a requesting user (webhook event runs) SHALL be gated as read-tier: they may use `read`-tier tools freely. When such a run reaches for a `write`-tier tool, the turn SHALL pause into the approval flow — an Owner or Admin approves or denies from the transcript — and the run SHALL resume or end accordingly. Approval reuses the existing interrupt/resume machinery.

#### Scenario: Webhook run reads freely, writes through approval

- **WHEN** a PR-opened event run summarizes the diff (read tier) and then offers to comment (write tier)
- **THEN** the read call executes, the write call pauses the turn for Owner/Admin approval, and the run resumes with the comment or ends with the denial recorded

### Requirement: Visible effective tiers

The agent config integrations section SHALL show, per attached connection, how many exposed tools are read-tier and how many are write-tier, so administrators can see what non-admin members can drive through the agent before an incident defines it.

#### Scenario: Tier counts on attach

- **WHEN** an admin opens the agent config integrations section with GitHub attached
- **THEN** the row shows the read/write tool split alongside the access level
