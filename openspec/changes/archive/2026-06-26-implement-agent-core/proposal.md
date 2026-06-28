## Why

onclaw's provider/secrets/store/onboarding layer is built and specced, but **the agent itself is a 3-line stub** (`internal/agent/agent.go`) and every provider adapter is a no-op stub — so `onclaw run` prints "not implemented" and no real inference ever happens. This change turns onclaw into a working on-device coding agent: a real tool-calling ReAct loop over a remote LLM, streaming output, builtin file/shell tools scoped to a workspace, a file-based persona system — folding in the four highest-value patterns catalogued in `docs/references/agent-reference-takeaways.md` (features A–D). It also adds a **named-agents** layer so a user can create more than one agent, each selecting its own model and reasoning effort.

## What Changes

- Replace the `internal/agent` stub with an eino ADK `ChatModelAgent` ReAct agent + a streaming runner (assistant tokens to stdout; tool calls/results surfaced).
- Add a **real OpenAI-compatible provider adapter** (covers OpenAI, Zhipu GLM, Ollama `/v1`, gateways via `api_base`) and register it for `openai`/`openai-compatible`/`ollama`; `anthropic` stays stubbed for this change. Uses eino-ext, with a hand-rolled `net/http` fallback if eino-ext can't coexist with the pinned eino v0.9.9.
- Add builtin tools `read_file`, `write_file`, `list_dir`, `shell`, implementing reference **Feature A** — shell exec policy `deny|allowlist|ask` with a command allowlist.
- Add a first-class **workspace** model: resolution (`--workspace` > `agent.workspace` > `ONCLAW_WORKSPACE` env > config > cwd), a tool-scoping boundary (path-traversal guards; `shell` runs with `cmd.Dir = workspace`), and prompt grounding (absolute path + project-type hint). Synthesized from the reference claws' workspace concept.
- Add a **layered persona/memory-file** system: a global `USER.md` at `~/.onclaw/USER.md` (user facts shared by all agents) plus per-agent files (`IDENTITY`/`SOUL`/`CAPABILITIES`/`USER`/`AGENTS`/`MEMORY.md`) inside each agent's workspace. `onclaw init` / `agent add` create them empty (non-destructive); `AGENTS.md` is seeded with default operating-instructions content.
- Add a **named-agents** layer: an `agents` table (provider ref, `model` + `reasoning_effort` overrides, `system_prompt`, tool subset, `max_iterations`, `workspace`) with CLI CRUD (`onclaw agent add|use|list|remove|show`) and a `default_agent` preference. `onclaw run`/`chat` select an agent via `--agent` or the default; per-run `--provider`/`--model`/`--reasoning` override the agent's choices. DB-stored (not files) for CRUD parity with providers.
- An agent **resolves to an effective provider profile**: copy the referenced provider profile, overlay the agent's `model` + `reasoning_effort`, then build via the existing adapter. Credentials stay in the provider profile; an agent row never holds a key.
- Each agent has an **agent-owned workspace** by default: `~/.onclaw/workspace/<name>/` (created on `agent add`), resolved just below `--workspace`. `--workspace` redirects the agent to an external project. (Named selectable agents are distinct from subagents, which remain out of scope.)
- Add a **builtin default "master" agent** that always exists (so `onclaw run`/`chat` work out of the box after provider setup) and **self-bootstraps**: on a fresh install it receives an **onboarding prompt** (a markdown file) that encourages it to ask the user questions, then records what it learns by editing its workspace persona/memory files with its normal file tools — no special tool. Persona/memory files are per-agent, living in each agent's workspace.
- Integrate reference **Feature B** (clean ctx-cancellation → an `interrupted` transcript line, no torn turn), **Feature C** (summarization middleware tuned to the 8192-token budget), and **Feature D** (no-secret rehydration at the tool-execution boundary).
- Minimal append-only per-turn transcript `.jsonl` (no full `sessions` table / `--resume` — deferred to M2).
- New CLI: streaming `onclaw run`; interactive `onclaw chat` REPL; `onclaw agent` CRUD. New flags `--workspace`, `--provider`, `--agent`, `--reasoning`.
- New bootstrap config keys: `workspace`, `tools.shell.policy`, `tools.shell.allowlist`, `agent.max_iterations`.

**Out of scope (later changes):** memory/recall/reflect (M2); plugin system / MCP / Lua (M3); full `sessions` table + `--resume`/`--continue`; Anthropic-native adapter; retry/failover wiring; **subagents / concurrent agents** (named selectable agents are in scope; concurrent/nested agents are not).

## Capabilities

### New Capabilities

- `agent-core`: eino ADK ReAct agent + streaming runner; context-budget summarization at the 8k limit; clean ctx-cancellation; minimal append-only transcript.
- `agent-tools`: builtin file tools (`read_file`/`write_file`/`list_dir`) and a `shell` tool with exec policy (`deny`/`allowlist`/`ask`); workspace-scoped file access; redaction-at-tool-boundary.
- `agent-workspace`: workspace resolution (`--workspace` > `agent.workspace` > env > config > cwd), the tool-scoping boundary, agent-owned default workspace, and prompt grounding.
- `agent-identity`: persona/context-file system (`IDENTITY`/`SOUL`/`CAPABILITIES`/`USER`/`AGENTS.md`) assembled into the system prompt, **layered with the selected agent's `system_prompt`**, with `onclaw init` seeding defaults.
- `agent-profiles`: named agents stored in an `agents` table, selectable per run; per-agent `model` + `reasoning_effort` resolved into an effective provider profile; agent-owned workspace default; agent CLI CRUD.

### Modified Capabilities

- `providers`: add a requirement that the `openai`/`openai-compatible`/`ollama` kinds resolve to a **real streaming ChatModel** that performs live inference (via eino-ext, with a hand-rolled fallback), rather than the current no-op stub, and that a normalized reasoning-effort value is mapped to the provider's native request field.
- `onboarding`: add an onboarding-prompt step to `onclaw init` (and first run) where a fresh agent asks the user questions and edits its own persona files with its normal file tools.

## Impact

- **Code:** `internal/agent/` (replace stub: assembler, runner, transcript, tools, context-loader, **agent→effective-profile resolution**), `internal/llm/adapter/` (real OpenAI-compatible adapter, **per-provider reasoning-effort mapping**), `internal/store/sqlite/` (**`agents` table + migration + CRUD**), `internal/cli/` (`run` rewrite, new `chat`, new `agent` command, `init` persona seeding, new flags), `internal/config/` (new keys).
- **Dependencies:** add `eino-ext/libs/acl/openai` (or none if hand-rolled). MUST preserve `CGO_ENABLED=0` static build and the ~2 GB RAM discipline (`Concurrency=1`, streaming, bounded 8k context, one agent running at a time). **Risk #1:** eino-ext's openai adapter may target an eino core version ≠ v0.9.9 (cached `components/model/openai@v0.1.13` pins v0.7.13) — resolved first; hand-roll fallback if incompatible.
- **CLI:** new `onclaw chat`; new `onclaw agent`; `onclaw run` changes from a placeholder print to a streaming response.
- **Specs:** introduces 5 new capability specs (incl. `agent-profiles`); adds requirements to `providers`. Adds a schema migration for the `agents` table (the prior "no migration in M1" no longer holds).
- **Basis:** plan at `.claude/plans/let-s-implement-the-agent-starry-rose.md`; references `docs/references/{nullclaw,goclaw,zeroclaw}.md` + `docs/references/agent-reference-takeaways.md`.