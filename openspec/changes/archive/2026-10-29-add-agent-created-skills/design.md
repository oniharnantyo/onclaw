# Design

## Context

Skills already resolve across two tiers read fresh per turn: the global library (`~/.buddies/skills/`, registry-indexed, read-only as a jail extra root) and the agent's workspace skills directory (`workspaces/<slug>/skills/`, inside the primary jail root, auto-created on demand — see `internal/agents/skills.go resolveSkills`). Agent-tier authoring is therefore mechanically possible today through the jailed file tools; the capability spec's design already anticipated it. What is missing is entirely around the mechanism: guidance, visibility, and a sharing path. The promote flow is a service-layer write into the global tier — the same posture as `save_document` for knowledge — never a jail change.

## Goals / Non-Goals

**Goals:**

- Agents reliably author valid skills into their own workspace tier.
- Personal skills are visible and manageable in the UI where the agent is already configured.
- Sharing is a deliberate, human-approved act that reuses the existing global-library gates untouched.

**Non-Goals:**

- No agent-facing write path into the global tier (no `create_skill` tool, no writable shared jail mount).
- No changes to skill resolution, precedence, the eligibility gate, or the store schema.
- No cross-agent peer reads of workspace skills; no per-agent enable state beyond the existing allowlist.
- No registry provenance field (cosmetic under this model; revisit if origin badges are wanted later).
- No agent-initiated promotion or agent-initiated delete of skills via a dedicated tool (workspace delete via its existing file tools is fine).

## Decisions

**D1 — Authoring is taught, not tooled.** The system prompt gains a skills-authoring contract paragraph alongside the memory and knowledge paragraphs (same composition seam, `memory_prompt.go` pattern): the conventions (one directory per skill under `skills/`, DNS-label directory name, `description:` frontmatter — it is what other turns see), when to save (repeated multi-step workflows, corrections that worked), and that skills are personal until promoted. A seeded `create-skill` global skill carries the full playbook, invoked on demand via `$create-skill`.

- *Why not a `create_skill` tool?* The jailed write tools already reach the only tier agents may author into; a tool would duplicate them and imply a global-write capability we explicitly reject. Teaching keeps one write path and one validation point (the existing discovery/parser behavior).
- *Why a seeded skill in addition to the paragraph?* The memory precedent: the paragraph stays short (always in context), the playbook (full conventions, examples, anti-patterns) loads only when authoring. Seeds are idempotent create-if-missing (`SeedSkill` pattern) so user- or agent-tailored versions stand.

**D2 — Promote is a copy, not a move.** Promoting copies the skill directory into `~/.buddies/skills/` and upserts the registry row; the workspace copy stays. The creator's copy continues to shadow the promoted skill for that agent (existing precedence), so promoting never changes what the creator sees, and a later library-side disable leaves the creator unaffected.

- *Alternative (move) rejected:* it severs the creator's shadow relationship and makes a global disable take the skill away from its author.
- *Consequence:* the copies diverge independently. Re-promoting overwrites the library copy (with confirm); the workspace copy is always the agent's live version.

**D3 — Collision rejects.** Promoting a name that already exists in the library fails with an error naming the collision; the library skill is untouched. The curated tier is not silently overwritten by a copy action, and shadowing from the workspace tier already gives an override path when someone truly wants a replacement.

**D4 — The UI lives in agent configuration, not the Skills pane.** AgentConfigPane gains a workspace-skills section beside its existing allowlist chips: the files this agent owns next to the library skills it includes. The Skills pane keeps meaning "the global library". Promotion from the agent's own surface also reads correctly — the skill belongs to that agent until shared. New service bindings back the list, promote, and delete actions.

- *Alternative (agent dropdown in the Skills pane) rejected:* it splits one agent's story across two panes and grows the global pane's surface for something that is not global.

**D5 — Delete is UI-side and workspace-only.** The delete action removes the workspace skill directory; the library is untouched. No confirmation beyond the pane's existing destructive-action pattern.

## Risks / Trade-offs

- [Promoted copy drifts from the agent's evolving workspace version] → Re-promote overwrites with confirmation; the pane could surface a "promoted" hint later. Acceptable for a snapshot-sharing model.
- [Agents write malformed skills (bad frontmatter, hostile names) that then fail discovery quietly] → The contract teaches the shape; discovery already skips invalid directories without breaking the turn. Name validation guidance mirrors the existing DNS-label regex.
- [Prompt budget grows by one more contract paragraph] → The paragraph is short by design; the bulk rides the seeded skill behind progressive disclosure.
- [Users expect promote to move, and are surprised the agent keeps its copy] → The pane labels the action Promote (copy) or shows the shadowing consequence once; wording settled during apply.

## Migration Plan

No store schema changes; no data migration. Seeds and prompt text apply on next build. Rollback is a plain revert — the only persisted state created by this change is ordinary skill directories and registry rows, both of which predate it.

## Open Questions

None blocking. Wording of the pane action label and the exact contract paragraph text are settled during apply.
