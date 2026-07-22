## Decision: keep `Instruction`, set `Description` — do not remove the system prompt

eino's `TypedChatModelAgentConfig` has two unrelated fields. From source (`adk/chatmodel.go:506-511`):
`Instruction` is "used as the system prompt … if empty, no system prompt will be used" and becomes
`schema.SystemMessage(instruction)` (`adk/chatmodel.go:314`). `Description` "helps other agents determine
whether to transfer tasks to this agent" (`adk/chatmodel.go:501-504`), consumed only by
`genTransferToAgentInstruction` (`adk/instruction.go`) and `NewAgentTool` (`adk/agent_tool.go:141-143`).

Removing `Instruction` (an option considered and rejected) would strip the model's system prompt entirely
— persona and grounding included — while `Description` would do nothing in onclaw's standalone setup. We
therefore **keep** `Instruction: b.instruction` and only **remove the agent's free-text field from
`b.instruction`**, repurposing the field as eino `Description` metadata. The system prompt remains
(persona + grounding); the field becomes legible agent metadata.

## Decision: system prompt = persona + grounding only

After the change, `buildPrompt()` concatenates, in order: the persona context (`LoadPersonaContext`:
global `USER.md` then per-agent files), the workspace-grounding line, and the tool-usage line. The agent's
`description` no longer appears in the system prompt. An agent with no persona files runs with grounding +
tool line only — an explicit, accepted consequence (the description is metadata, not a prompt).

## Decision: rename the column with `ALTER TABLE … RENAME COLUMN`

The codebase already runs `ALTER TABLE llm_providers DROP COLUMN …` (`internal/store/sqlite/db.go:343`),
which requires SQLite ≥ 3.35; `RENAME COLUMN` (≥ 3.25) is therefore safe under the bundled
`modernc.org/sqlite`. The migration is guarded by the existing `columnExists` helper (`db.go:493`) for
idempotency: rename only when `system_prompt` exists and `description` does not.
`CREATE TABLE IF NOT EXISTS agents` is updated to `description` so fresh installs match. Existing agent
text is preserved.

## Decision: clean rename, no JSON/CLI back-compat

The JSON key (`system_prompt` → `description`) and CLI flag (`--system-prompt` → `--description`) change
in lockstep with the web UI and tests. No dual-key read fallback. This keeps the schema and API honest;
onclaw is pre-1.0 and single-user.

## Decision: leave the input-safety middleware naming alone

`internal/agent/middlewares/input_safety_middleware.go` uses `systemPromptTokens` /
`NewInputSafetyMiddleware(systemPromptTokens, …)` to name the **token budget** of the system prompt
(estimated from `b.instruction`). That concept is unchanged — `b.instruction` is still a system prompt —
so the internal naming stays. Only the agent config field and its eino target change.