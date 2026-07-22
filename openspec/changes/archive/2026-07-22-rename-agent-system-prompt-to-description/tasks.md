# Implementation Tasks

## 1. Data model rename (Go)

- [x] 1.1 `internal/store/types.go`: `Agent.SystemPrompt` → `Agent.Description`.
- [x] 1.2 `internal/store/sqlite/agent.go`: column `system_prompt` → `description` in INSERT,
  SELECT-by-name, SELECT-list, UPDATE (column list + `&a.Description` scan).
- [x] 1.3 `internal/api/service/types.go`: `json:"system_prompt"` → `json:"description"` in `AgentView`
  and `AgentInput`; field → `Description`.
- [x] 1.4 `internal/api/service/agent.go`: field mapping (`SystemPrompt` → `Description`) in
  Create/Get/List/Update.
- [x] 1.5 `internal/cli/agent_cmd.go`: flag `--system-prompt` → `--description` (keep `-` stdin), local
  var, struct field, `"System Prompt:"` → `"Description:"` print.

## 2. SQLite migration

- [x] 2.1 `internal/store/sqlite/db.go`: `CREATE TABLE` column → `description`.
- [x] 2.2 Add idempotent migration: if `columnExists(agents, "system_prompt")` and not
  `columnExists(agents, "description")`, run `ALTER TABLE agents RENAME COLUMN system_prompt TO description`.

## 3. Agent runtime (behavior change)

- [x] 3.1 `internal/agent/agent.go` `buildPrompt()`: remove the `AgentConf.SystemPrompt` append; keep
  persona + grounding + tool line.
- [x] 3.2 `internal/agent/agent.go` eino config: add `Description: b.opts.AgentConf.Description`; keep
  `Instruction: b.instruction`.
- [x] 3.3 `internal/agent/agent.go` debug log: `"system_prompt"` / `SystemPrompt` → `"description"` /
  `Description`.

## 4. Web UI

- [x] 4.1 `web/src/components/Agents.tsx`: `system_prompt: string` → `description: string`.
- [x] 4.2 `web/src/pages/AgentDetailPage.tsx`: default value, form load, label (`System Prompt` →
  `Description`), bindings (`agentForm.description` / `set('description')`).

## 5. Tests

- [x] 5.1 `internal/store/sqlite/agent_test.go`, `internal/api/server_test.go`,
  `internal/cli/cli_test.go` (`--description`), `internal/agent/runner_test.go`,
  `internal/llm/service_test.go` — rename field/flag/column references; keep assertions.
- [x] 5.2 Sweep `go build ./...` for remaining breakages.

## 6. Verification

- [x] 6.1 `make build && make vet && make lint`.
- [x] 6.2 `make test` green; affected packages ≥ 70%.
- [x] 6.3 Migration: existing DB renames `system_prompt` → `description` with data preserved; fresh DB
  creates `description`.
- [x] 6.4 Runtime: an agent with non-empty `description` and no persona files sends a system message
  containing grounding but **not** the description; the eino config carries `Description`.
- [x] 6.5 UI: the "Description" field round-trips under the `description` JSON key.
- [x] 6.6 `openspec validate rename-agent-system-prompt-to-description`.