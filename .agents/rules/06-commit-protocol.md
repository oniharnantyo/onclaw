---
description: "Conventional commits and commit hygiene"
---
# Commit protocol

- **Conventional Commits** — format `type(scope): subject`, e.g. `feat(note): add note creation`, `docs(rules): add handover rule`, `fix(http): map validation errors to 400`.
- types allowed: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `ci`, `build`, `chore`, `revert`.
- `scope` = the package or area (`http`, `config`, `rules`, `docker`, `ci`).
- Subject: English, imperative ("add"), ≤ 72 chars, lowercase after scope.
- One logical change per commit; no magnet-shots, no formatting-only commits mixed with logic.
- When rule files changed: edit `rules/`, regenerate with `make rules`, and include both — never commit derived files edited by hand.
- Do not commit generated binaries, `.env`, or coverage artifacts.
- If a commit would break `make test`, split it — broken intermediate commits are not allowed.
