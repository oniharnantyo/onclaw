## Why

The agent's `system_prompt` field does double duty today: it is stored agent config **and** it is
concatenated into the eino agent's system prompt. In `internal/agent/agent.go`, `buildPrompt()` joins
`AgentConf.SystemPrompt` + the persona context (`USER.md`, `SOUL.md`, …) + workspace grounding into one
string and hands it to `adk.TypedChatModelAgentConfig.Instruction`.

Verified from eino source (`cloudwego/eino@v0.10.0-alpha.12`, `adk/chatmodel.go:501-511`,
`adk/chatmodel.go:314`, `adk/instruction.go`, `adk/agent_tool.go:141-143`):

- `Instruction` becomes `schema.SystemMessage(instruction)` — it **is** the model's system prompt.
- `Description` is consumed **only** for agent-to-agent transfer and `NewAgentTool`; it is inert in
  onclaw's standalone setup today.

onclaw conflates the two. We want the agent's free-text field to be lightweight **metadata** (a
description of what the agent is for), decoupled from the system prompt — which is already fully
assembled from the persona files + grounding. Renaming `system_prompt` → `description` makes that intent
legible and lets the field populate eino's `Description`, ready for future multi-agent routing.

## What Changes

- **Rename `SystemPrompt` → `Description`** across the data model: store type, SQLite column, HTTP JSON
  key, CLI flag, agent runtime references, and web UI type/label.
- **Data-preserving DB migration**: `ALTER TABLE agents RENAME COLUMN system_prompt TO description`
  (guarded by `columnExists`); new installs create the column as `description` directly.
- **Stop folding the field into the system prompt**: `buildPrompt()` builds `b.instruction` from persona +
  workspace grounding + tool line only. `Instruction: b.instruction` is kept — the system prompt still
  reaches the model.
- **Pass the field to eino `Description`**: the agent builder sets `Description: b.opts.AgentConf.Description`
  on `TypedChatModelAgentConfig`.
- **No backward-compat shims**: clean rename of the JSON key and CLI flag.

## Capabilities

### Modified Capabilities

- `agent-profiles`: the `agents` table stores `description` (not `system prompt`); the
  `agent add`/`edit` flag is `--description` (not `--system-prompt`). The `description` is agent metadata
  passed to the eino agent's `Description`, not a system-prompt layer.
- `agent-identity`: the agent's `description` is **not** part of the assembled system prompt. The system
  prompt is the global `USER.md` + per-agent persona/memory files + workspace grounding. The `description`
  is passed as the eino agent's `Description` (agent-to-agent routing metadata).
- `web-ui`: the agent identity form lists `description` (not `system prompt`); the field stays a free-text
  `<textarea>`.

## Impact

**Affected code:**

- `internal/store/types.go` — `Agent.SystemPrompt` → `Agent.Description`.
- `internal/store/sqlite/agent.go` — column `system_prompt` → `description` in INSERT / SELECT-by-name /
  SELECT-list / UPDATE.
- `internal/store/sqlite/db.go` — `CREATE TABLE` column + the rename migration.
- `internal/api/service/types.go`, `internal/api/service/agent.go` — `json:"description"` and field mapping
  in Create/Get/List/Update.
- `internal/cli/agent_cmd.go` — flag `--description`, local var, struct field, print label.
- `internal/agent/agent.go` — `buildPrompt()` (drop the field from `b.instruction`), eino config (add
  `Description`), debug log key.
- `web/src/components/Agents.tsx`, `web/src/pages/AgentDetailPage.tsx` — type field, label, form binding,
  default value.
- Tests: `internal/store/sqlite/agent_test.go`, `internal/api/server_test.go`, `internal/cli/cli_test.go`,
  `internal/agent/runner_test.go`, `internal/llm/service_test.go`, plus breakages surfaced by
  `go build ./...`.

**Affected systems:** agent configuration storage, CLI, HTTP API, agent runtime (instruction assembly),
web UI.

**Dependencies:** none new.

**Non-goals:** removing the eino `Instruction` (kept — it is the system prompt); adding JSON/CLI
backward-compat; wiring multi-agent `NewAgentTool` (the `Description` is set now and becomes useful when
that lands); changing persona/memory file handling, grounding text, or the input-safety middleware.