## Why

When an agent executes a tool that returns a large output (e.g. `web.fetch` reading a comprehensive article), Eino's `reduction` middleware attempts to offload the full response to disk and provide a truncated preview with a `read_file` pointer. However, OnClaw configured the reduction middleware with the agent's physical host path (`cfg.Filesystem.AgentDir`), causing it to write raw host absolute paths (e.g. `/Users/.../trunc/<callID>`) to the jailed filesystem backend. The jail backend strictly rejects raw absolute host paths for write operations (`resolveWritablePath`), resulting in a `NodeRunError` that terminates the agent run with "Run failed".

Setting `RootDir` to the virtual mount point `backend.DefaultMountPoint` (`/workspace`) ensures the reduction middleware generates mount-scoped virtual paths (`/workspace/trunc/<callID>`) which resolve cleanly through the jail to the agent directory on disk, while also giving the model a valid path it can actually read back using `read_file`.

## What Changes

- Update `reduction.TypedConfig.RootDir` in `internal/agents/agent.go` to use `backend.DefaultMountPoint` (`/workspace`) instead of `cfg.Filesystem.AgentDir`.
- Large tool outputs and cleared conversation turns are now safely offloaded to `/workspace/trunc/<callID>` and `/workspace/clear/<callID>` without tripping jail security guards.
- Agent models receive working virtual file pointers (`/workspace/trunc/<callID>`) that they can inspect using standard `read_file` calls.

## Capabilities

### New Capabilities
<!-- None -->

### Modified Capabilities
- `agent-runtime`: Clarify that tool reduction offload (truncation and clear phases) operates inside the filesystem jail under `/workspace` without triggering jail rejection errors.

## Impact

- **Affected code:** `internal/agents/agent.go` (composition of reduction middleware).
- **APIs / Specs:** No wire protocol or API changes. Seamless transparent fix for tool execution context preservation.
