# Tasks

## 1. Server: bound-profile resolution (`internal/browseruse`)

- [x] 1.1 Add meta keys `buddies.dev/profile-dir` and `buddies.dev/profile-name` (constants beside `MetaScreenshotDir`) and parse them off the call's `_meta` in the handler seam; verify with a table test that absent/present/garbage meta parse to the expected binding
- [x] 1.2 Route every tool handler's profile resolution through the D3 seam: when `profile-dir` is bound, validate it is a clean absolute path, `MkdirAll` it (`0700`), key the sessions map by it, ignore the `profile` argument (no `ValidateBrowserProfile` on it), and use `profile-name` for display echo only; verify `go test ./internal/browseruse/` covers bound-resolution for `navigate`, `cookies`, `screenshot`, and the takeover/secret handlers
- [x] 1.3 Extend `profileExists` so a bound profile always passes (the dir is auto-created, not required to pre-exist) while named profiles keep today's refusal; verify with a takeover test: `__request_takeover` with a bound dir and no on-disk profile returns ready, not `profile-missing` (bound passes because the seam's `MkdirAll` runs before `profileExists`; pinned by `TestHandleRequestTakeover_BoundDir`)
- [x] 1.4 Add a takeover relaunch test under binding: `__begin_takeover` relaunches headed on the bound directory, the headed session's cookie jar exports there at teardown, and the next bound action reuses the login; verify via `go test ./internal/browseruse/ -run Takeover`

## 2. Workspace: agent-level profile dir (`internal/workspaces`)

- [x] 2.1 Add `DirBrowserProfile = "browser-profile"` and a `BrowserProfileDir(slug)` helper (validated slug, mkdir -p on demand with `dirPerm`, no filesystem access on invalid slug); verify with unit tests mirroring `SkillsDir`/`TasksDir` in `workspaces_test.go`

## 3. Parent: stamp the binding (`internal/agents`)

- [x] 3.1 Resolve the agent's profile dir and display name at the tool-listing site and carry them on `sessionDirTool`; stamp `buddies.dev/profile-dir` + `buddies.dev/profile-name` into the `_meta` only for browser-use server tools; verify with a listing test that browser tools carry both keys and computer-use tools carry neither
- [x] 3.2 Extend `invokeChildTool` to accept and send `_meta`, and pass the binding on the reserved-channel calls (`__request_takeover`, `__begin_takeover`, `__resolve_attention`, `__request_secret`, `__deliver_secret`); update the attention card prompt to name the agent rather than a profile literal; verify with the attention flow tests in `internal/agents` that the channel invocations carry the agent's binding

## 4. Integration + regression

- [x] 4.1 End-to-end test at the agents layer: two agents' browser tool calls resolve to distinct profile directories, neither session observes the other's cookies, and an unbound direct `browser-mcp` call still resolves `~/.buddies/browser/profiles/<name>` (regression for the fallback); verify with `go test ./internal/agents/... ./internal/browseruse/`
- [ ] 4.2 Run `gofmt -w .`, `go vet ./...`, `go test ./...` clean; live-smoke: create/rename nothing, use two agents browsing the same site, confirm separate logins and per-agent takeover sign-in persistence (headed window lands on the bound profile; jar at `~/.buddies/workspaces/<slug>/browser-profile/buddies-cookies.json`), then restart the app and confirm the login survives — gate part DONE fresh (gofmt/vet/test all clean); live-smoke part owed to the user
