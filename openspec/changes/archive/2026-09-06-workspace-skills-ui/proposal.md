## Why

Skills exist end-to-end at the runtime layer (three-tier discovery, progressive disclosure via the eino skill middleware) but are unmanageable in the product: there is no API or UI to install, inspect, enable, or remove a workspace skill, the settings Skills pane operates on mock data, and the only way to add a skill is hand-placing directories on the server. Meanwhile the per-agent `disabled_skills` denylist forces every agent's prompt to carry every workspace skill's metadata and contradicts how the product thinks about capabilities. This change makes skills a first-class managed resource with a deliberate activation model and direct `$skill-name` invocation from any conversation.

## What Changes

- **New `workspace_skills` registry** (PostgreSQL): one row per workspace-level skill (name, display name, description, version, source, `enabled`, timestamps) with the multi-file body on disk under the existing workspace skills directory. System skills stay embedded-and-synced; no rows.
- **Automatic workspace-tier flow**: an *enabled* workspace skill is attached to **every agent in the workspace automatically**. There is no per-agent skill toggle at any tier. **BREAKING**: the agent `disabled_skills` field is removed from the schema, API, and runtime; the denylist model is replaced by tier-level control.
- **Tier control model**: system tier always injected, locked, cannot be disabled anywhere (fork-to-workspace to customize); workspace tier controlled only by the workspace-level `enabled` master switch and uninstall; agent tier visible only to its agent, managed by install/remove on that agent.
- **Install sources** (all require `skills.write`, i.e. Owner/Admin/Superadmin): author in place (SKILL.md body editor), upload a folder/zip preserving bundled `scripts/`/`references/`, and fetch from a git URL or archive URL. Forking a system skill into the workspace tier comes from the same flow.
- **Install-time dependency report**: each install reports unmet prerequisites instead of failing at run time — tool names the skill references vs the workspace tool gate/agent allowlist, and runtimes needed by bundled scripts vs the shell's PATH.
- **`$skill-name` invocation**: the runner recognizes explicit `$name` skill mentions in user input on **all execution surfaces** (direct chats, channels, cron prompts) and converts them into a blocking instruction to invoke the skill middleware's `skill` tool — the middleware remains the single execution path so invocations render as tool-call cards.
- **UI**: Settings → Skills pane rebuilt on the real API (install wizard with source selection + dependency report, library list with master switches, Members read-only); agent config Step 3 skill toggles replaced by a read-only locked inventory (system + enabled workspace) plus agent-tier skill management; composer gains a `$` skill menu following the existing `/` and `@` menu pattern.
- **Skills HTTP API returns** (previously removed routes): workspace skills CRUD + enable/disable under `/workspaces/{slug}/skills`, gated by `skills.write` for mutations, `skills.read` for listings. Permission catalog unchanged — Members keep read-only access at every tier.

## Capabilities

### New Capabilities
- `workspace-skills`: the workspace skill library — registry model, install sources (author/upload/git-URL/fork), enable/disable and uninstall, dependency reporting, permission gating, and the `$skill-name` invocation contract.

### Modified Capabilities
- `agents`: **BREAKING** — `disabled_skills` is removed from the agent schema/API; agents no longer carry per-agent skill state (spec currently mandates the denylist and forbids allowlists).
- `agent-runtime`: "Three-tier skills" requirement changes — attachment is governed by tier rules (system always; workspace by registry `enabled`; agent by presence) instead of the agent's `disabled_skills`, and explicit `$name` invocation is honored on every execution surface.
- `web-app/settings`: Skills pane requirement rewritten — real-API install wizard (author/upload/git-URL), dependency report, master enable/disable, uninstall, role-gated actions with Member read-only.
- `web-app/agents`: agent wizard Step 3 skill toggles replaced by locked inventory chips (system + enabled workspace) and agent-tier skill management for writers.
- `web-app/chat`: composer gains the `$` skill-invocation menu (grouped system/workspace/agent lists) alongside the existing `/` and `@` menus.

## Impact

- **Backend**: new `workspace_skills` table + migration; new store port and postgres adapter; skills HTTP handlers/routes (re-introduced with new semantics); `skillBackend` loses `disabledSkills` filtering and gains registry-aware workspace-tier gating; runner gains `$name` input parsing; system-skill fork support. `domain.Agent` loses `DisabledSkills`; workspace-creation `StarterAgent.DisabledSkills` removed.
- **Frontend**: `SkillsPane`/`SkillDialog` rebuilt on the API; `AgentConfigModal` Step 3 skill section replaced; `Composer` gains the `$` menu; `api.ts` skills CRUD rewired to the real contract; seed data updated.
- **Runtime behavior**: every enabled workspace skill's metadata is injected into every agent's progressive-disclosure context — the workspace master switch is the only volume control (accepted trade-off).
- **Tests/smoke**: agents tests referencing `disabled_skills`; smoke.sh section 13.1 (currently failing since the old skills routes were removed) rewritten against the new contract.
