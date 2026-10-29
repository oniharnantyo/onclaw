# Proposal

## Why

Skills are a live capability, but nothing in the product lets an agent grow one: the workspace skills directory is writable and picked up live, yet agents are never taught the authoring convention, and skills an agent creates are invisible to the user — the registry and the settings panes only know the global tier. Today an agent's hard-won workflow evaporates at the end of a session, and sharing it with another agent means hand-copying files on disk.

## What Changes

- Agents are taught to author skills: the system prompt gains a skills-authoring contract paragraph (the memory-contract precedent) teaching the convention — one directory per skill under the agent's own `skills/` directory, DNS-label directory name, `description:` frontmatter — and that saved skills are personal to that agent until promoted. A seeded `create-skill` global skill carries the full authoring playbook, loaded on demand via `$create-skill`.
- The agent's configuration lists the skills in that agent's own workspace skills directory — name, description, source tier — alongside its existing skill allowlist, so personal skills are visible and manageable.
- A Promote action copies a workspace skill into the global skills library and registers it, making it available to every agent under the existing eligibility gate (global enable, allowlist, empty-permits-all). Promote is a copy: the creator keeps its own version, which continues to shadow the promoted one. Promoting over an existing global name is rejected. A Delete action removes a workspace skill.
- Skill resolution, precedence (agent > global), the eligibility gate, and the jail's read-only global tier are unchanged. No new agent write path outside the workspace is introduced; the agent tier stays the only tier agents author into.

## Capabilities

### New Capabilities

<!-- none: this change extends existing capabilities only -->

### Modified Capabilities

- `agent-skills`: new requirements for agent-authored skills (the authoring contract, personal-until-promoted visibility, live pickup) and for promotion of a workspace skill into the global library (copy semantics, collision rejection).
- `agent-management`: agent configuration gains a requirement to list the agent's workspace-tier skills and offer promote and delete actions on them.

## Impact

- Backend (`internal/agents`): a skills-authoring contract paragraph joins the memory/knowledge paragraphs in system-prompt composition; a seeded `create-skill` skill rides the existing seed pattern (`internal/memory/skill.go SeedSkill`); service bindings to list an agent's workspace-tier skills, promote one into the global library, and delete one (manager already holds `workspaces` and `skillsDir`); `SkillRegistry.Upsert` on promote; regenerate Wails bindings.
- Frontend (`frontend/src/features/agents`): AgentConfigPane gains a workspace-skills section (list, promote, delete) beside the existing allowlist chips; tests in both themes.
- Unchanged: `internal/agents/backend` skill resolution and eligibility, the filesystem jail's root/extra-root policy, the store schema.
