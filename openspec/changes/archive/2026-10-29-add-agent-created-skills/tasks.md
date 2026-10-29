# Tasks

## 1. Backend — skills-authoring contract in the system prompt (spec agent-skills: Agent skill authoring)

- [x] 1.1 `internal/agents`: a `skillsGuidance` paragraph (the `knowledgeGuidance`/
  `memoryGuidance` precedent, same composition seam) teaching the authoring
  convention — one directory per skill under the workspace `skills/` directory,
  DNS-label directory name, `SKILL.md` with a `description:` frontmatter (it is
  what other turns' skill listings show) — and that saved skills are personal to
  the agent until promoted, with `$create-skill` named as the full playbook.
  Composed only on turns whose skill backend resolved (no orphan guidance).
- [x] 1.2 `internal/agents`: seeded `create-skill` global skill (`SkillName`,
  `skillContent`, `SeedCreateSkill(skillsDir string) error`, repair-if-deleted,
  mirroring `memory.SeedSkill`): the full playbook — conventions, when to save,
  worked example, anti-patterns — behind progressive disclosure.
- [x] 1.3 Tests: seed idempotency (existing file never overwritten), the
  contract rides the composed instruction when the skills capability is on and
  is absent when it is off, pinned composition order holds.

## 2. Backend — service bindings for workspace skills (spec agent-management: Agent workspace skills management; spec agent-skills: Skill promotion / Skill sharing boundary)

- [x] 2.1 `internal/services`: `AgentsService` gains the global skills-library
  directory (constructor-injected from the layout at the composition root) and
  three bindings — `ListWorkspaceSkills` (name + description per workspace
  skill directory), `PromoteWorkspaceSkill` (copy the skill directory into the
  global library, register it via `SkillRegistry.Upsert` enabled, leave the
  workspace copy untouched; a name that already exists in the library is
  rejected with an error naming the collision and changes nothing),
  `DeleteWorkspaceSkill` (remove the workspace skill directory only).
  Skill-directory names are validated (DNS-label) before any path use.
- [x] 2.2 Tests: list reads the agent's workspace tier only; promote copies
  (bytes present in the library, registry row created, workspace intact);
  collision rejects with the library skill byte-identical and no registry
  change; delete removes the workspace directory and leaves the library
  untouched; unknown agent rejected.
- [x] 2.3 Regenerate Wails bindings (`task common:generate:bindings`).

## 3. Backend — composition-root wiring

- [x] 3.1 `internal/cli/app.go`: seed the `create-skill` skill beside the other
  seeds (logged, not fatal) and pass the layout's skills directory to
  `NewAgentsService`.

## 4. Frontend — workspace-skills section in agent configuration (spec agent-management: Agent workspace skills management)

- [x] 4.1 `frontend/src/features/agents`: AgentConfigPane capabilities tab
  gains a workspace-skills section beside the skill allowlist chips — each
  workspace skill with name and description, a Promote action and a Delete
  action; promote success and the collision rejection are both surfaced
  (existing toast pattern); the list refreshes after every action; the
  section renders legibly in both themes with the existing tokens.
- [x] 4.2 Tests: list rendering, promote success and collision paths, delete,
  empty state — following the pane's existing test conventions.
