# Proposal

## Why

The live 20-step retrieval detour (2026-09-28) showed agents lack on-demand procedural guidance for the reference-document workflow: the base prompt carries one short paragraph (always-on token budget), while the deep procedure — manifest first, scoped reads via read hints, citation form, multi-document delegation, the never-shell-hunt rule — lives nowhere an agent can consult at the moment of need. The system-skill tier exists for exactly this (`internal/agents/systemskills`, the `web-research` precedent): always injected, progressive-disclosure content, fork-to-customize, zero registry changes. The user asked for a system skill for document read.

## What Changes

- New embedded system skill `document-read` in `internal/agents/systemskills/` beside `web-research`: teaches the reference-document workflow — discover via the manifest, `document.search`, scoped `document.read` (pages/section, and the per-hit read hints once `fix-reference-document-retrieval` lands), cite by name + locator linking `references/<name>`, delegate heavy multi-document research to the `agent` tool, and never hunt documents through the shell.
- No new mechanism: the existing `SyncSystemSkills` mirror at boot (`cli/server.go:165`), the system-tier injection rules, and the fork path all apply unchanged — adding the embedded directory is the whole surface.
- Relationship to `fix-reference-document-retrieval` task 4.1: the one-line base-prompt guardrail stays (always-on, one sentence); the skill carries the deep procedure on demand. The skill's wording is written to be correct before and after the read-hints land.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `workspace-reference-documents`: ADDED requirement — the runtime SHALL ship a system-tier `document-read` skill teaching the reference-document workflow, mirrored by the existing embedded-system-skills sync, forkable per the system-tier rules.

## Impact

- `internal/agents/systemskills/document-read/SKILL.md` — new embedded content.
- `internal/agents/systemskills/sync.go` — embed directive gains the new directory (one line).
- Sync/fork tests extended; no API, schema, store, or frontend change.
- Coordination: content references the read hints from `fix-reference-document-retrieval`; wording written to hold either side of that change's landing.
