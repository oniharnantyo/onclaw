## Context

See `proposal.md` for motivation.

Eino's `reduction` middleware (`github.com/cloudwego/eino/adk/middlewares/reduction`) manages tool outputs and context tokens in two phases:
1. **Truncation phase**: Intercepts tool results exceeding `MaxLengthForTrunc` (default 50,000 chars), saves full output to `${RootDir}/trunc/${tool_call_id}` on the configured `Backend`, and replaces the tool result with a preview and a `read_file` pointer.
2. **Clear phase**: Intercepts context when tokens exceed `MaxTokensForClear` (default 160,000 tokens), offloads historical tool arguments/results to `${RootDir}/clear/${tool_call_id}` on the `Backend`, and replaces them with placeholders.

OnClaw configures the `reduction` middleware with `jail` (`internal/agents/backend/fs_jailed_backend.go`) as its `Backend`. The jail strictly enforces virtual mount security:
- `resolveWritablePath` only allows write operations to paths under `/workspace` or mounted roots like `/project`.
- Raw physical host absolute paths (e.g. `/Users/...`) are rejected with `"absolute paths are not allowed: ... (write under /workspace instead)"`.

Because `agent.go:204` passed `RootDir: cfg.Filesystem.AgentDir` (the host physical path), every offload attempt failed in the jail and crashed the run.

## Goals / Non-Goals

**Goals:**
- Ensure Eino's `reduction` middleware offloads large tool results and cleared context turns to virtual mount paths under `/workspace` without tripping jail rejections.
- Maintain transparent containment: files land in the agent's directory on disk (`<agentDir>/trunc/...` and `<agentDir>/clear/...`) via the jail's existing `unmount` resolution.
- Enable the agent model to read truncated outputs back via `read_file(path="/workspace/trunc/<callID>")`.
- Add regression test coverage verifying that large tool outputs are successfully truncated, offloaded to the jailed backend, and retrievable via `read_file`.

**Non-Goals:**
- Do not modify Eino ADK internals or change reduction middleware thresholds (`MaxLengthForTrunc`, `MaxTokensForClear`).
- Do not reorder the entire middleware stack or alter how hooks interact with other middlewares.
- Do not modify the jail's path rejection rules (`resolveWritablePath` is correct to deny raw host paths).

## Decisions

### Decision: Set `reduction.TypedConfig.RootDir = backend.DefaultMountPoint`
- **Choice**: Pass `backend.DefaultMountPoint` (`"/workspace"`) as `RootDir` to `reduction.NewTyped`.
- **Rationale**: 
  - `filepath.Join("/workspace", "trunc", callID)` yields `/workspace/trunc/<callID>`, which is an absolute virtual path under the primary mount.
  - The jailed backend's `unmount("/workspace/...")` strips `/workspace` and maps the relative remainder `trunc/<callID>` into `cfg.Filesystem.AgentDir`.
  - The resulting persisted output notice `<persisted-output>Full output saved to: /workspace/trunc/<id> ... Use read_file to view</persisted-output>` gives the LLM a valid path that passes the jail's read check.
- **Alternatives Considered**:
  - *Custom `GenTruncOffloadFilePath` / `GenClearOffloadFilePath` functions*: Would achieve the same `/workspace/trunc/...` path formatting, but `RootDir: backend.DefaultMountPoint` is simpler, cleaner, and uses Eino's standard path generator natively.
  - *Allow host paths in jail's `resolveWritablePath`*: Breaks the tenant isolation abstraction and allows path escape vulnerabilities. Rejected.

## Risks / Trade-offs

- **[Risk]** The `trunc/` and `clear/` folders will be created inside the agent's directory on disk.
  - *Mitigation*: This is expected and clean. They are scoped directly to that agent's storage and accessible through `read_file` or `ls`.

- **[Risk]** Multiple large tool calls in a session could consume disk space.
  - *Mitigation*: These files are plain text files bounded by session lifetime / agent directory retention policies.
