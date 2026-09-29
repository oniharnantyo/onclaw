# Proposal

## Why

The BOOTSTRAP.md birth ritual (introduce yourself, show your vibe, invite a first task, then delete the file) was a persona cold-start aid that no longer earns its cost: it occupies a permanent slot in every instruction composition, a tab in the agent Prompts UI, seeding/cleanup code across promptdocs/promptgen/handlers, and a visible file in every agent's jailed `/workspace` — all for a one-time ritual the base prompt and IDENTITY/SOUL personas already cover. It is also the only prompt doc whose content is a fixed template rather than generated, making it a standing special case in every layer that touches prompt documents.

## What Changes

- **BREAKING**: remove the `BOOTSTRAP.md` birth sequence entirely — the embedded template, seeding on agent create and workspace starter-agent create, the composer's eighth document slot, the `bootstrap` field on the agent domain/API projection, the read-only Prompts-tab entry in the web agent modal, and all test/smoke assertions.
- Extend the startup sweep to also remove `BOOTSTRAP.md` and `BOOTSTRAP.md.bak` from existing agent workspace directories, so the birth-ritual text no longer sits readable inside the agent's jailed `/workspace` mount after upgrade.
- Keep `SeedWorkspace`'s clear-list semantics (a reused slug directory must not inherit a stale `BOOTSTRAP.md`); keep the session "birth title" feature, which is input-derived and independent of the file; keep `delete_file` unchanged (no special case exists).
- Spec drift resolved: the `agent-prompts` requirement described an LLM-generated personalized bootstrap while the code seeds a fixed template — the requirement is removed outright rather than reconciled.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-prompts`: REMOVED — "BOOTSTRAP.md birth sequence"; MODIFIED — "Prompt documents are workspace files" (workspace documents are `IDENTITY.md`/`SOUL.md` only; the read-side composes `identity` and `soul`; the startup sweep additionally removes stray `BOOTSTRAP.md`/`.bak`).
- `agent-runtime`: MODIFIED — composition order drops `BOOTSTRAP.md` (channel doc position becomes "between USER.md and the end of the workspace documents"); the jailed-filesystem requirement stops citing `BOOTSTRAP.md` deletion.
- `agents`: MODIFIED — the agent field list drops the `bootstrap` server-managed file-backed field.
- `web-app/agents`: MODIFIED — the Prompts tab lists IDENTITY.md and SOUL.md only; the read-only bootstrap preview goes away.
