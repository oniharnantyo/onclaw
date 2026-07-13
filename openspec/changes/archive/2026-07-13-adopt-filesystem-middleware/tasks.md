# Tasks: Adopt Eino Filesystem Middleware

## Backend (`internal/agent/tools/fsbackend.go`)
- [x] Add `fsBackend` struct (`workspace string`) + `NewFSBackend(workspace)`.
- [x] Implement `filesystem.Backend`: `LsInfo`, `Read`, `GrepRaw`, `GlobInfo`,
      `Write`, `Edit` over `os`/`filepath`/`regexp`.
- [x] Every method calls `ValidatePath(workspace, req.Path/FilePath)` first;
      reject `..` / absolute escapes.
- [x] Redact outbound text: `FileContent` body and `GrepMatch` line text via
      `Redact(...)`. Leave `FileInfo.Path` unmasked.
- [x] `Edit` honours `EditRequest.ReplaceAll` — false ⇒ error unless exactly
      one literal match; `Read` honours `Offset`/`Limit` (1-based, default 2000).
- [x] `GrepRaw` honours `GrepRequest` pattern/glob/fileType/context lines;
      `GlobInfo` honours `GlobInfoRequest.Pattern`/`Path`.

## Shell (`internal/agent/tools/fsshell.go`)
- [x] Add `fsShell` struct + `NewFSShell(workspace, policy, allowlist, denylist)`.
- [x] Implement `filesystem.Shell.Execute(ctx, *ExecuteRequest)` mapping
      `ExecuteRequest.Command` → today's shell logic.
- [x] Port the `deny`/`denylist`/`allowlist`/`ask` switch verbatim from
      `shell.go`.
- [x] Reuse `matchesCatastrophic`, `isAllowedCommand`, `CappedBuffer`,
      `denylistReCache`, `catastrophicCategory`, `internal/shellpolicy` — move
      them into `fsshell.go`, then delete `shell.go`.
- [x] Run `sh -c` with `cmd.Dir = workspace`, 32 KB `CappedBuffer` cap.
- [x] Map result to `ExecuteResponse{Output: Redact(out), ExitCode: …,
      Truncated: …}`.

## Wiring (`internal/agent/agent.go`)
- [x] Construct `filesystem.NewTyped[*schema.AgenticMessage]` with
      `Backend: NewFSBackend(workspace)`, `Shell: NewFSShell(...)`, no
      `ToolConfig` name overrides.
- [x] Add the filesystem middleware to `Handlers` (after summarization; before
      skill/hooks — verify ordering at runtime).
- [x] Remove the `init()` registrations from `readfile.go`, `writefile.go`,
      `editfile.go`, `listdir.go` (then delete the files — see Cleanup).

## Toggle middleware (`internal/agent/middlewares/fs_toggle_middleware.go`)
- [x] Typed middleware embedding
      `*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]`.
- [x] Override `WrapInvokableToolCall` + `WrapEnhancedInvokableToolCall`; read
      the tool name from `*ToolContext` (confirm field in `adk/handler.go`).
- [x] If name is one of the seven fs tools and `EnabledChecker.Enabled(name)`
      is false, return an endpoint yielding `"tool <name> is disabled"`.
- [x] Add it to `Handlers` **after** the filesystem middleware.

## Registry seeding
- [x] Add `tools.FSToolMetadata() []ToolMeta` returning the seven rows:
      Filesystem = `ls`, `read_file`, `write_file`, `edit_file`, `glob`,
      `grep`; Shell = `execute`.
- [x] Locate the startup seeding site (consumer of `tools.GetRegistry()` that
      fills `tool_registry`) and extend it to seed these rows with
      `enabled = 1`.

## Cleanup (step 7 of plan)
- [x] Delete `internal/agent/tools/{readfile,writefile,editfile,listdir,shell}.go`.
- [x] Delete their `_test.go` counterparts.
- [x] `grep` the package for stragglers; `make vet` must be clean.

## Tests
- [x] `fsbackend_test.go`: path-traversal block; read offset/limit; edit
      exact-match (zero/one/many); redaction of `sk-…`/`nvapi-…` in read +
      grep output; `glob` and `grep` happy paths.
- [x] `fsshell_test.go`: `deny`/`denylist`/`allowlist`/`ask` policies;
      catastrophic-pattern block + named reason + no exec; 32 KB cap;
      redaction of secret in output; denylist match logged.
- [x] `fs_toggle_middleware_test.go`: a tool flagged disabled in
      `EnabledChecker` is blocked; an enabled one passes through.
- [x] Keep `internal/agent/tools` and `internal/agent/middlewares` ≥ 70 %
      coverage.

## Verification
- [x] `make vet && make fmt` clean.
- [x] `go test ./internal/agent/tools/... ./internal/agent/middlewares/...
      ./internal/agent/...` green.
- [x] `make build` (static, `CGO_ENABLED=0`).
- [x] `openspec validate adopt-filesystem-middleware --strict` passes.
- [ ] Manual (`onclaw run` or chat UI): `read_file`, `glob`, `grep`, `execute`
      succeed; `../../etc/passwd` read blocked; `deny`/`denylist` still reject
      catastrophic patterns; disabling `glob` in `tool_registry` blocks it on
      the next run.
- [ ] Confirm the combined system prompt is not duplicated
      (`CustomSystemPrompt` left nil ⇒ middleware default acceptable).
