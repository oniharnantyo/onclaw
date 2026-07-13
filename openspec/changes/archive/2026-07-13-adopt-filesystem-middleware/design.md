# Design: Adopt Eino Filesystem Middleware

## Decision: middleware owns the tool surface, onclaw owns the semantics
Use Eino's `filesystem.NewTyped[*schema.AgenticMessage]` to inject
`ls / read_file / write_file / edit_file / glob / grep / execute`, but supply
onclaw's own `filesystem.Backend` and `filesystem.Shell` implementations. The
`Backend`/`Shell` interface is the seam: Eino defines the shape of each request
and response; onclaw decides what a read/write/edit/grep/execute actually does
— confinement, redaction, policy.

### Why adopt the middleware rather than keep hand-rolled tools
The five existing tools overlap ~70% of the middleware's surface. The only
genuinely new capability is `glob` + `grep`; the rest is reimplementation that
drifts from upstream. Adopting the middleware retireures the overlap under a
maintained interface and adds `glob`/`grep` "for free", while the
`Backend`/`Shell` implementations preserve every behavioural guarantee onclaw
currently makes.

### Why implement Backend/Shell ourselves (not wrap eino-ext/local)
`eino-ext/adk/backend/local` is not resolvable at the current pins, and even if
it were, its out-of-workspace behaviour and lack of redaction are unknown.
Implementing `Backend` directly over `os`/`filepath` lets onclaw bake
`ValidatePath` + `Redact` in at the source — the same code paths the existing
tools use — with no decorator layer and no upstream behaviour to audit.

### Why no tool-name overrides
`ToolConfig.Name` can rename any tool, and `ExecuteToolConfig` embeds it, so
`ls`→`list_dir` and `execute`→`shell` were technically possible. We choose not
to: accepting Eino's defaults keeps onclaw's tool surface identical to every
other Eino deployment, avoids rename maintenance, and the spec is amended to
match. (`ToolConfig.Disable` / `CustomTool` remain available for future
per-deployment tuning.)

## Redaction placement
Today every builtin tool is wrapped by `tools.WrapRedacted` in
`tools.Builtin()` (`registry.go:27`), masking secret patterns in both arguments
and results. Middleware-injected tools bypass `Builtin()`, so for the seven
filesystem tools the redaction moves **into** the `Backend`/`Shell` methods:

- `Backend.Read` redacts the returned `FileContent` body.
- `Backend.GrepRaw` redacts matched line text in each `GrepMatch`.
- `Shell.Execute` redacts `ExecuteResponse.Output`.
- `LsInfo` / `GlobInfo` return paths only — paths are not masked.

This preserves the invariant that no plaintext secret reaches the model or the
recorded history from a file/shell operation. Non-filesystem builtin tools
(`memory`, `kg_search`, `web/*`, etc.) continue to flow through `Builtin()` and
keep the decorator; their redaction is unchanged.

## Tool management: enable/disable without a registry Tool
The `tools-management` spec requires every builtin to declare a category, seed
into `tool_registry`, and be toggleable. Middleware tools inject outside
`Builtin()`, so two pieces restore this:

1. **Seeding.** A `tools.FSToolMetadata()` returns the seven `{Name, Desc,
   Category}` rows (Filesystem: `ls`, `read_file`, `write_file`, `edit_file`,
   `glob`, `grep`; Shell: `execute`). The startup seeding site that already
   consumes `tools.GetRegistry()` is extended to seed these rows with
   `enabled = 1`, so the management API/UI continue to list and group them.
2. **Enforcement.** A typed toggle middleware embeds
   `*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]` and
   overrides `WrapInvokableToolCall` / `WrapEnhancedInvokableToolCall`. For a
   tool name whose `tool_registry` enable flag is false, it substitutes an
   endpoint that returns `"tool <name> is disabled"`. It reuses the existing
   `tools.EnabledChecker` (`registry.go:22`) and the `inMemoryEnabledChecker`
   already built in `AssembleAgent`. It is placed **after** the filesystem
   middleware in `Handlers` so it wraps the middleware's tools.

The result: toggling a filesystem tool in the UI takes effect on the next agent
run without a restart — the same property the spec mandates for registry tools.

## Cleanup
Once `fsbackend.go` and `fsshell.go` carry the logic, `readfile.go`,
`writefile.go`, `editfile.go`, `listdir.go`, and `shell.go` (plus their
`_test.go` files) are unreferenced. `shell.go`'s reusable policy helpers
(`matchesCatastrophic`, `isAllowedCommand`, `CappedBuffer`, the denylist cache,
the `catastrophicCategory` map, and the `internal/shellpolicy` dependency) are
consolidated into `fsshell.go` before `shell.go` is deleted. Test coverage is
relocated: the path-traversal, edit-exact-match, policy, cap, and redaction
assertions move onto `fsbackend_test.go` / `fsshell_test.go` so coverage in
`internal/agent/tools` stays ≥ 70%.

## Compatibility with existing specs
- `agent-tools`:
  - "Builtin file tools operate within the workspace" is MODIFIED — `list_dir`
    → `ls`, and `glob`/`grep` are added to the mandated set; confinement and
    edit-exact-match behaviour are unchanged.
  - "The shell tool enforces an execution policy" is MODIFIED — `shell` →
    `execute`; all policy modes (`deny`/`denylist`/`allowlist`/`ask`) and
    scenarios are preserved.
  - "Secrets are not rehydrated at the tool-execution boundary" is MODIFIED —
    redaction for file/shell tools is applied inside the `Backend`/`Shell`;
    other builtin tools keep the decorator.
  - ADDED — file/shell tools are provided by the Eino filesystem middleware
    backed by a workspace-confined, redacting `Backend`/`Shell`.
- `tools-management`:
  - "Global tool enable/disable" is MODIFIED — the enable flag also governs
    middleware-injected tools via the toggle middleware.
  - ADDED — middleware-injected file/shell/glob/grep tools seed into
    `tool_registry` with categories Filesystem/Shell.

## Risks
- **Two changes modify the shell-policy requirement** (this one and
  `revise-shell-tool-policy`). Both deltas must be archived in a coordinated
  order; this delta encodes the full end-state to minimise clobber.
- **Middleware ordering vs `hooks_middleware`.** If onclaw relies on the hooks
  middleware seeing file/shell tool calls, the toggle/hooks middleware must be
  placed so it wraps after the filesystem middleware. Verified at integration.
- **`ToolContext` field name** for the tool name inside `WrapInvokableToolCall`
  is confirmed against `adk/adk/handler.go` at implementation time.
- **`ask` policy** still blocks on stdin — CLI-only, unchanged from today.
