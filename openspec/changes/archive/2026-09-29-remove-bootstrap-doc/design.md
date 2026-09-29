# Design

## Context

BOOTSTRAP.md today is: an embedded template (`promptdocs.BootstrapTemplate`) seeded — never LLM-generated, despite the spec's older wording — by `promptgen.GenerateForCreate` and the workspace starter-agent path (`handlers/workspaces.go`); the composer's eighth document (`runner.go` Compose, attended profile only); a server-managed file-backed field projected onto every agent read (`domain.Agent.Bootstrap`, `handlers/agents.go` `composePromptDocuments`, web `api.ts` type); a read-only third entry in the web Prompts tab; and a real file sitting in each agent's jailed `/workspace` (the template instructs the agent to delete it after the ritual). The session "birth title" (`runner.go` `sessionTitle`) is input-derived and unrelated. The `internal/bootstrap` package (master tenant) and web theme "bootstrap" are name collisions only.

## Goals / Non-Goals

**Goals:**
- Remove the feature end-to-end: template, seeding, composition slot, API projection, web tab, tests, smoke assertions.
- Sweep stray `BOOTSTRAP.md`/`.bak` files out of existing agent workspaces at startup — the file is agent-readable inside the jail, so leaving it would keep the ritual text visible to models.

**Non-Goals:**
- Replacing the ritual with another cold-start mechanism (IDENTITY/SOUL personas + the enhanced base prompt cover it).
- Touching the birth-title session feature, `delete_file`, or the master-tenant `internal/bootstrap` package.
- Prompt-catalogue changes (owned by `enhance-agent-base-prompt`; the base prompt never referenced the ritual).

## Decisions

- **D1 — Sweep, don't just stop seeding.** Extended `SweepSeededBasePrompts` (renamed or paired) also deletes `BOOTSTRAP.md`/`BOOTSTRAP.md.bak` per agent dir, logged per removal. Rationale: the jailed mount makes the leftover file model-visible; stopping the seed alone leaves stale ritual text discoverable at `/workspace/BOOTSTRAP.md`.
- **D2 — Keep the `SeedWorkspace` clear-list entries for bootstrap filenames.** A reused slug directory must not inherit a stale file; clearing costs two `os.Remove` calls and no special cases beyond the list.
- **D3 — Remove the API field outright (BREAKING).** Pre-1.0 internal API; an always-empty deprecated `bootstrap` field buys nothing. Read-side projection (`composePromptDocuments`) drops to identity/soul; `ReadPromptDocuments` loses its third return value.
- **D4 — Spec drift resolved by removal.** The old requirement described LLM-generated personalized bootstrap; the code seeded a fixed template. Rather than reconcile, the requirement is REMOVED and the sweep scenario added where the file-lifecycle rules live.

## Risks / Trade-offs

- Agents mid-ritual at upgrade time lose the ritual text from composition; the sweep may delete the file mid-conversation. Harmless: the ritual is explicitly not a gate, and composition treats missing documents as absent.
- ~20 test files reference bootstrap; the smoke suite pins template text in two agent-read assertions (814, 2847). All mechanical updates.

## Migration Plan

Startup sweep only — no schema or data migration; the database never stored bootstrap content.

## Open Questions

None.
