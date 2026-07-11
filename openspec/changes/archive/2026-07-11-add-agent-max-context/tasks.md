# Tasks

## 1. Schema and store

- [x] 1.1 Add `MaxContextTokens int` to `store.Agent` in `internal/store/types.go`.
- [x] 1.2 Add a guarded migration in `internal/store/sqlite/db.go`: `ALTER TABLE agents ADD COLUMN max_context_tokens INTEGER NOT NULL DEFAULT 0` behind a `columnExists(db, "agents", "max_context_tokens")` check, mirroring the `reasoning_budget_tokens` migration.
- [x] 1.3 Extend the sqlite agent store (`internal/store/sqlite/agent.go`) `AddAgent`, `GetAgent`, `ListAgents`, `UpdateAgent` to read/write `max_context_tokens` (column list, `VALUES`, `SELECT`, `UPDATE SET`, and `Scan`).
- [x] 1.4 Tests: add/round-trip an agent with a non-zero `max_context_tokens`; confirm `0` default for existing/unset rows; confirm `UpdateAgent` persists it (black-box, ≥ 70% coverage of the touched package).

## 2. Service + DTO

- [x] 2.1 Add `MaxContextTokens int` to `AgentInput` and `AgentView` (`internal/api/service/types.go`) with the `max_context_tokens` JSON tag.
- [x] 2.2 Map the field through `CreateAgent`, `UpdateAgent`, `GetAgent`, `ListAgents` in `internal/api/service/agent.go`.
- [x] 2.3 Tests: handler/service round-trip of `max_context_tokens` through the JSON API (≥ 70% coverage of touched packages).

## 3. Resolution precedence

- [x] 3.1 In `internal/cli/agent_session.go`, resolve the effective context window as agent override → global config → model default: `if agent.MaxContextTokens > 0` use it; else if `st.cfg.MaxContextTokens > 0` use that; else keep the model's discovered context window. Preserve the existing global override behavior when the agent value is `0`.
- [x] 3.2 Verify an agent with `max_context_tokens = 0` behaves exactly as today (global default wins); an agent with a non-zero value overrides both global and model default.

## 4. CLI flag

- [x] 4.1 Add `--max-context` (int) to `onclaw agent edit` (and `onclaw agent add` if consistent with `--max-iterations`), validated as `>= 0`; `0`/omitted retains the inherited value.
- [x] 4.2 Tests for the flag (set, clear, partial-update preserves other fields).

## 5. Web UI

- [x] 5.1 Add `max_context_tokens: number` to the `Agent` interface (`web/src/components/Agents.tsx`) and to `DEFAULT_FORM` + the edit-load mapping in `web/src/pages/AgentDetailPage.tsx` (default `0`).
- [x] 5.2 Add an **"Override max context" checkbox** to the Overview form. Derive its checked state from `max_context_tokens > 0` on load. When unchecked, the number field is disabled/hidden and the agent inherits the global default; when checked, the number field is enabled.
- [x] 5.3 Add the structured number field (label, tooltip, min `1`, inline validation, hint showing the global default), rendered/enabled only while the checkbox is checked. It MAY pre-fill with the selected model's discovered context window when the override is first enabled.
- [x] 5.4 Toggling the checkbox off SHALL set `max_context_tokens` to `0` on save (inherit); toggling on SHALL persist the entered value. The checkbox maps to the existing `0 = inherit` sentinel — no new storage field or DTO boolean.
- [x] 5.5 Ensure the value round-trips through save (load → edit → save → reload): a stored non-zero value re-opens with the checkbox checked and the field populated; a `0` value re-opens unchecked. Send `0` explicitly when unchecked (avoid the JSON `undefined`-omission footgun).
- [x] 5.6 `cd web && npm run build && npm run lint`; manually verify the full edit cycle — check → set value → save → reopen (still checked); uncheck → save → reopen (unchecked, field disabled, inherits global).

## 6. Docs and build

- [x] 6.1 Fix the stale `max_context_tokens: 8192` reference in `CLAUDE.md` → `64000` (actual default in `internal/config/defaults.go`).
- [x] 6.2 `make build && make vet && make test`.
