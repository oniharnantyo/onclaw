---
description: "Documentation rules; AGENTS.md and CLAUDE.md stay minimal"
---
# Documentation rules

- **`AGENTS.md` and `CLAUDE.md` always stay minimal**: pointers and a few critical lines only. Rules content lives in `rules/` — never duplicate rules into these files.
- **`rules/` is the source of truth**: edit rules only there, then regenerate derived copies with `make rules`. Never edit `.claude/rules/` or `.cursor/rules/` by hand.
- New endpoint or changed API behavior → update `docs/openapi.yaml`.
- New concept/layer/slice → update `docs/architecture.md` (including the canonical slice section).
- User-facing changes → update `README.md`.
- Docs must match reality: when behavior changes, docs change in the same change as the code.
- English only. Keep `.env.example` in sync with config structs (add new env keys with a one-line comment).
