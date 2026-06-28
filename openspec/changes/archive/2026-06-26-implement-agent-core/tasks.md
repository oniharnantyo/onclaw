# Tasks — implement-agent-core

## 1. Phase 0 — Dependency resolution (Risk #1)

- [x] 1.1 Attempt `go get github.com/cloudwego/eino-ext/libs/acl/openai@latest`; verify it coexists with eino v0.9.9 (`go mod tidy` + `make build`, no CGO).
- [x] 1.2 If MVS conflicts with eino v0.9.9, decide on the hand-rolled `net/http` OpenAI ChatModel and record the decision (do not silently downgrade eino core).
- [x] 1.3 Confirm exact eino API from the module cache: `AsyncIterator.Next()`/`StreamReader.Recv()`, the `compose.ToolsNodeConfig` tools field, the summarization `Trigger` shape, and `tool.InvokableTool` / `utils.InferTool`.
- [x] 1.4 Confirm the reasoning-effort request field for the target OpenAI-compatible endpoint(s) (Open Question #4).

## 2. Real OpenAI-compatible adapter (`providers`)

- [x] 2.1 Implement `internal/llm/adapter/openai_compat.go`: `Build(ctx, *store.Profile, apiKey)` → real streaming `model.ChatModel` from `{APIBase→BaseURL, Model, Settings}`.
- [x] 2.2 Map a normalized `settings.reasoning_effort` (`low|medium|high`) to the provider's native request field; fail closed (send no effort) if the value is unrecognized or unsupported by the kind.
- [x] 2.3 Register it for `openai`/`openai-compatible`/`ollama` in `defaults.go`; keep `anthropic` on the stub.
- [x] 2.4 Unit tests: profile→config mapping (no network); disabled profile errors; reasoning-effort mapping.

## 3. Workspace model (`agent-workspace`)

- [x] 3.1 Add `workspace` config key + a resolution helper: `--workspace > agent.workspace > ONCLAW_WORKSPACE env > config > cwd` → absolute, no `os.Chdir`.
- [x] 3.2 Add `--workspace` flag to `run` and `chat`.
- [x] 3.3 Unit tests: resolution order (incl. agent default) + absolute path normalization.

## 4. Builtin tools (`agent-tools`)

- [x] 4.1 Implement `read_file`/`write_file`/`list_dir` as workspace-scoped `tool.InvokableTool`s (path-traversal guards).
- [x] 4.2 Implement the `shell` tool: policy `deny`/`allowlist`/`ask`, `cmd.Dir = workspace`, ctx cancellation, output cap.
- [x] 4.3 Add `tools.shell.policy` / `tools.shell.allowlist` config keys.
- [x] 4.4 Implement the redaction-at-tool-boundary guard (no secret rehydration; mask known secret patterns in args/results).
- [x] 4.5 Unit tests: traversal block; shell deny/allowlist/ask; redaction no-rehydrate + masking.
- [x] 4.6 Restructure the builtin tools into an extensible `internal/agent/tools/` sub-package (spec'd in `agent-tools`): define the `Tool` interface + `Scope` (`tools.go`), a `registry.go` + `Builtin(scope)` factory, one file per tool (`readfile`/`writefile`/`listdir`/`shell`), shared `redaction.go` (decorator + exported `Redact`) and `pathguard.go`; wire `agent.AssembleAgent` to `tools.Builtin` and `transcript.go` to `tools.Redact`; mirror tests per file. Supersedes the single-file layout of 4.1–4.2.

## 5. Persona system (`agent-identity`)

- [x] 5.1 Implement `internal/agent/context.go` loader: global `USER.md` (home root) + per-agent `IDENTITY`/`SOUL`/`CAPABILITIES`/`USER`/`MEMORY.md` + `AGENTS.md` (workspace root); skip missing; fixed order; byte cap. Expose as the persona layer of the system prompt.
- [x] 5.2 Ship per-file templates under `internal/agent/templates/` (`identity`/`soul`/`capabilities`/`user`/`memory`/`agents.md`, embedded); `onclaw init` and `onclaw agent add` seed each persona file from its matching template (and global `~/.onclaw/USER.md` from `user.md`) — only when absent (non-destructive).
- [x] 5.3 Unit tests: assembly order; missing-skipped; seeding preserves existing files.

## 6. Agent core (`agent-core`)

- [x] 6.1 Implement `internal/agent/agent.go` assembler: `Deps` + `Instruction` (persona layer + **selected agent's `system_prompt`** + workspace grounding + base instruction) + summarization `Handler` (trigger ≈ 6000 tokens). The model is built from the **effective profile** resolved in §9.
- [x] 6.2 Implement `internal/agent/runner.go`: drive `Run().Next()`, stream assistant tokens to an `io.Writer`, surface tool calls/results, honor ctx cancel → write `interrupted`.
- [x] 6.3 Implement `internal/agent/transcript.go`: append-only `.jsonl`, `fsync` per turn, redact secrets, never hold full session in memory.
- [x] 6.4 Add an offline scripted/fake `model.ChatModel` for tests (extend the `internal/llm` fake pattern).
- [x] 6.5 Unit tests: streaming to a buffer; tool dispatch; cancel → `interrupted` line; summarization trigger fires; transcript events in order.

## 7. CLI wiring

- [x] 7.1 Rewrite `internal/cli/run.go`: resolve agent (§9) + workspace, build model from the effective profile (`mgr.Build`), `agent.New`, stream to stdout, write transcript.
- [x] 7.2 New `internal/cli/chat.go` REPL: one turn per line; clean Ctrl-C/Ctrl-D; shell `ask`-confirm wired; `/agent <name>` and `/reasoning low|medium|high` slash commands.
- [x] 7.3 Register `chat` and `agent` in `internal/cli/app.go`.

## 8. Verification & hardening

- [x] 8.1 `make fmt`, `make vet`, `make test` green; ≥80% coverage on new packages.
- [x] 8.2 `make build` produces a static `CGO_ENABLED=0` binary; no CGO transitive deps.
- [x] 8.3 Manual E2E against a real/local OpenAI-compatible endpoint: `run` streams + uses a tool; `chat` shell `ask`-confirm; Ctrl-C exits cleanly with an `interrupted` line; an Ollama `/v1` base URL works; selecting an agent with a different model/reasoning effort changes behavior.

## 9. Agent profiles (`agent-profiles`)

- [x] 9.1 Schema migration: `CREATE TABLE agents (...)`; store DTO (`internal/store/types.go`) + interface (`internal/store/store.go`) + sqlite impl (`internal/store/sqlite/agent.go`) CRUD; hot-reload wiring (reuse the existing reload-pending flag).
- [x] 9.2 Agent resolution → effective profile: fetch referenced provider profile, copy it, overlay agent `model` and `reasoning_effort` (into `settings`), return the effective `*store.Profile`. Validate the referenced provider exists and is enabled.
- [x] 9.3 `onclaw agent add <name> [--provider p] [--model m] [--reasoning low|medium|high] [--workspace path] [--system-prompt -]` → insert row + create `~/.onclaw/workspace/<name>/`. `agent list|show|remove`. `agent use <name>` sets `default_agent`.
- [x] 9.4 Selection + flags: `--agent <name>` on `run`/`chat` (else `default_agent`, else error if none); per-run `--provider`/`--model`/`--reasoning` override the agent's values; precedence = explicit flags > agent row > provider-profile defaults.
- [x] 9.5 Layered system-prompt assembly (persona files + `agent.system_prompt` + grounding); honor the agent's tool subset and `max_iterations` (0 = config default).
- [x] 9.6 Unit tests: CRUD; effective-profile merge (override + fallback); disabled/missing provider errors; layered prompt order; tool-subset filtering.

## 10. Master agent & onboarding prompt

- [x] 10.1 Seed a builtin default "master" agent (always present; initial `default_agent`); ensure `onclaw run`/`chat` succeed out of the box against it.
- [x] 10.2 Onboarding-prompt injection in the assembler: prepend the onboarding prompt while an `onboarding_completed` preference is unset; mark onboarding complete (set the preference) when the onboarding interaction concludes.
- [x] 10.3 Ship the default onboarding prompt content (the "just woke up → ask the user → edit your persona files with your file tools" prompt); the agent records what it learns via the normal `write_file`/`edit_file` tools — no special tool.
- [x] 10.4 `onclaw init` offers the onboarding step (appended after provider setup, skippable); it simply runs the master agent fresh so the onboarding prompt fires.
- [x] 10.5 Unit tests: builtin master exists and is default; onboarding prompt injected when persona is empty and absent once populated; agent edits workspace persona files via the normal file tools.