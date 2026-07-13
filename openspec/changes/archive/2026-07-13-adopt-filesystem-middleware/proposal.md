# Proposal: Adopt Eino Filesystem Middleware for File and Shell Tools

## Intent
Replace onclaw's five hand-rolled registry tools (`read_file`, `write_file`,
`edit_file`, `list_dir`, `shell`) with the Eino `filesystem` ADK middleware
(`github.com/cloudwego/eino/adk/middlewares/filesystem`), which injects a
superset — `ls`, `read_file`, `write_file`, `edit_file`, `glob`, `grep`,
`execute` — backed by a pluggable `Backend` + `Shell` interface.

The win is twofold: the agent gains **structured `glob` and `grep` as
first-class tools** (today only reachable by shelling out), and onclaw aligns
with the upstream **Backend abstraction** — the seam Eino uses for local and
sandboxed (AgentKit) execution. onclaw keeps full control of behaviour by
implementing `Backend` and `Shell` itself.

## Problem
- onclaw reimplements five tools that Eino already provides, plus their
  request/response shapes, prompt guidance, and the edit exact-match dance.
  Carrying that code means drifting from upstream as Eino evolves the tool
  surface.
- There is no structured `glob` or `grep` tool. The agent must shell out
  (`shell` with `find`/`grep`), which is subject to the shell policy and
  returns unstructured output; under a `deny`/`allowlist`/`ask` policy a
  search may be blocked entirely.
- Redaction, path confinement, and the shell policy today live in onclaw's
  `tools.Builtin()` → `WrapRedacted` → registry pipeline. A naive "just enable
  the middleware" would bypass all three, because middleware tools inject at
  `BeforeAgent`, outside that pipeline.

## Proposed Solution
**Adopt the typed middleware.** Construct
`filesystem.NewTyped[*schema.AgenticMessage]` (the same typed-middleware
pattern onclaw already uses for summarization) and add it to the agent's
`Handlers`. Accept Eino's default tool names verbatim — `ls`, `read_file`,
`write_file`, `edit_file`, `glob`, `grep`, `execute` — no renaming.

**onclaw implements `Backend` and `Shell`.** The middleware owns only the tool
surface (schema, name, prompt); onclaw owns the semantics. The `Backend` runs
every file op through the existing `ValidatePath` (workspace confinement) and
`Redact` (secret masking); the `Shell` ports today's policy verbatim —
`deny`/`denylist`/`allowlist`/`ask`, the catastrophic-pattern floor, the 32 KB
output cap — and redacts its output. Because Eino's `execute` tool is a pure
pass-through to `Shell.Execute`, no policy is lost.

**Restore tool management.** Middleware-injected tools bypass `Builtin()`, so
enable/disable and category grouping are re-established two ways: a thin typed
toggle middleware (`WrapInvokableToolCall` / `WrapEnhancedInvokableToolCall`)
enforces the `tool_registry` enable flag at call time, and the seven tools are
seeded into `tool_registry` (categories `Filesystem` / `Shell`) so the
management API and UI continue to show and group them.

**Delete the superseded tools.** Once the middleware injects the seven tools
and `Backend`/`Shell` carry the logic, the five original registry tool files
become dead code and are removed; their test coverage is relocated onto the new
`fsbackend_test.go` / `fsshell_test.go`.

## Constraints & Dependencies
- **Spec coordination with `revise-shell-tool-policy`.** That pending change
  adds the `denylist` policy mode to the "shell tool enforces an execution
  policy" requirement; this change renames that requirement's tool `shell` →
  `execute`. Both delta the same requirement, so the two changes must be
  archived in a coordinated order; this change's delta reflects the full
  intended end-state (`execute` + `deny`/`denylist`/`allowlist`/`ask`).
  `openspec validate --strict` will surface any merge conflict at archive time.
- **No eino-ext dependency required.** `eino-ext/adk/backend/local` is not in
  the module cache; onclaw implements `Backend` directly over `os`/`filepath`.
  This is preferable — confinement and redaction are baked in at the data
  source rather than decorating an upstream backend of unknown confinement
  behaviour.
- **Behaviour preserved.** Workspace path confinement, `edit_file` exact-match
  semantics, secret redaction, and the full shell policy (including the
  catastrophic floor and `ask` on CLI/stdin) are unchanged in behaviour; only
  the tool names (`list_dir` → `ls`, `shell` → `execute`) and the implementation
  seam change.
- **Low-resource target.** The `Backend`/`Shell` are thin wrappers over
  `os`/`os.exec` and existing helpers; no new processes or parsers beyond what
  today's tools already use.
- **Tool naming.** Eino's default names are accepted as-is. The `agent-tools`
  spec is amended to reference `ls` and `execute` instead of `list_dir` and
  `shell`.

## Out of Scope (Deferred)
- **AgentKit / sandbox backend.** The `Backend` abstraction makes a future
  sandbox backend a drop-in, but wiring one is a separate change.
- **DeepAgent adoption.** `adk/prebuilt/deep` bundles planning + sub-agents on
  top of a filesystem backend; this change only adopts the filesystem
  middleware on the existing `ChatModelAgent`.
- **`reduction` / large-tool-result offloading integration.** The middleware
  can offload large results via the `reduction` middleware; not wired here.
- **Per-agent shell policy.** Policy stays global (as today); per-agent policy
  is the same deferred item as in `revise-shell-tool-policy`.
- **Multi-modal read** (`UseMultiModalRead`) — left at the default (off).
