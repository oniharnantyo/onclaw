# Design

## Context

The system-skill substrate is complete and exercised: `internal/agents/systemskills` embeds skill directories (`web-research/SKILL.md` today), `SyncSystemSkills` mirrors them into `domain.SystemSkillsDir(onClawDir)` at boot (`cli/server.go:165`), the workspace-skills spec's system tier governs injection (always, non-disableable, fork-to-customize), and the skills middleware exposes them to every agent with progressive disclosure. Content format: `# Title`, `name:`, `description:` lines, then markdown with Available Tools and Workflow sections — `web-research` is the template.

## Goals / Non-Goals

Goals: one new embedded skill dir whose content encodes the document workflow, including the lessons from the 2026-09-28 live detour; content accurate before and after `fix-reference-document-retrieval` lands; zero mechanism changes.

Non-Goals: no new skill source, registry column, or sync behavior (the substrate already covers system tier); no per-agent attach (system tier is universal); no embedding of document *content* — the skill teaches procedure only; no replacement of the base-prompt reference-documents paragraph (that stays; the skill adds depth on demand).

## Decisions

- **D1 — Name: `document-read`, matching the user's framing and the tool family.** The description covers the full workflow (search + read + cite), so the narrower name doesn't undersell it. *Alternative:* `reference-documents` — rejected: collides mentally with the capability name and the settings pane label.
- **D2 — Content follows the `web-research` template exactly**: title/name/description header, Available Tools (`document.search`, `document.read` with pages/section), Workflow (numbered), and a Citing section. Detour lessons baked in: consult the manifest before searching blind; a hit's read hint IS the next call; `references/<name>` is the only path form an agent needs; no Glob/find/unzip — ever; heavy multi-document research delegates to the `agent` tool (the base prompt already teaches this delegation).
- **D3 — Hint-agnostic wording.** The skill says "each hit names the document, its locator, and — when present — the exact `document.read` call", so the content is true today and stays true when `fix-reference-document-retrieval` adds the `read` field. No cross-change ordering constraint.
- **D4 — Sync is already content-complete.** The `go:embed` directive gains `document-read/SKILL.md`; `SyncSystemSkills`'s mirror semantics (overwrite on diff, remove extraneous) then manage the deployed tree, and the existing fork path is the customization story. No registry rows: system tier has none by design.

## Risks / Trade-offs

- [Always-injected token cost] → progressive disclosure means the middleware advertises name + description, not the body; cost is one description line per agent, same class as `web-research`.
- [Skill and base-prompt paragraph drift apart] → both live in-repo; the skill's Workflow section is the superset, and the base prompt's one-line form is restated inside the skill so each is self-sufficient.
- [Content accuracy depends on tool naming] → the names `document.search` / `document.read` / `references/` are the stable family contract; content tests assert them.

## Migration Plan

Content-only addition; `SyncSystemSkills` reconciles existing instances on next boot. Rollback = revert the embed addition; the sync removes the extraneous directory on the following boot.

## Open Questions

- None.
