# add-agent-skills

Add a **skills** subsystem to the onclaw agent: tiered runtime resolution via
eino's `adk/middlewares/skill`, normalize-on-install from external sources
(GitHub / skills.sh, HTTP archive, local path, Claude plugin), and management
through the CLI, the JSON API, and the web console.

Artifacts:

- [proposal.md](proposal.md) — why, what changes, capabilities, impact
- [tasks.md](tasks.md) — phased implementation checklist
- [design.md](design.md) — eino reuse, multi-dir backend, compatibility, install UX
- [specs/agent-skills/spec.md](specs/agent-skills/spec.md) — `ADDED Requirements` delta