# Design

## Context

Browser Use proved the system-MCP pattern end to end: a supervised stdio child (`internal/cli/browser_mcp.go`), an engine package (`internal/browseruse`), a synthesized system row (`internal/agents/mcp/systemserver.go`), parent-side attention interception, and the `_meta` screenshot jail. Computer Use reuses every one of those bones. The OS layer itself is not built here: both reference implementations agree a native desktop-control layer is its own project (ZCode ships a Node SEA helper + N-API addon; OpenBot vendors a Rust binary), and OpenBot's vendored `cua-driver` (trycua/cua, MIT) is the proven, digest-pinned, notarized option.

Verified facts driving the decisions (from the 2026-11-06 exploration):
- The daemon socket protocol is custom line-delimited JSON; the *stable documented* integration is `cua-driver mcp --socket <daemon>` spoken over stdio as MCP (mcp-go client works).
- The daemon enforces no cross-client serialization — "one controller" is prose. Concurrency is the embedder's problem.
- Telemetry is ON by default and embedded mode does not disable it; the env names carry an `_RS_` infix (`CUA_DRIVER_RS_TELEMETRY_ENABLED`, `CUA_DRIVER_RS_UPDATE_CHECK`); `DO_NOT_TRACK=1` covers both.
- macOS embedding contract (EMBEDDING.md): spawn `serve --embedded` as a direct child of the signed app (TCC attribution), never re-sign the vendor binary, restart the daemon after grant changes.
- eino/ADK tool messages are text-only (`schema.Message.Content`); images reach models only via user-role blocks (`ContentBlockTypeUserInputImage`). `sanitizeModel` establishes the decorator pattern that changes only the provider's view.
- Tool call arguments already ride to the frontend (`ToolActivity.Arguments`), so a trajectory UI needs no new event plumbing — but `type` text rides verbatim with it.
- An empty agent allowlist means all servers (`mcp.go:126`): a second always-on system server would silently widen every existing agent.

## Goals / Non-Goals

Goals:
- Desktop observe-and-act for agents, darwin first, windows/linux wired behind the same adapter.
- Permission-gated enablement with zero tools before grants.
- Deterministic arbitration: one desktop, one actor.
- Pixels in the model's context without forking connectors or breaking replay.

Non-Goals:
- Building a native accessibility/input layer (vendored).
- Multi-display capture beyond the primary display (driver limitation, documented).
- Recording/replay of action trajectories, driver session APIs, `run_script`/`run_steps` batching (open vendor issues #5897/#5910 hit out-of-process integrators exactly like us), and the AGPL `cua-perception` extension.
- Headless/virtual-display operation; server-mode desktop control.
- Browser automation changes of any kind.

## Decisions

### D1 — Vendor `cua-driver`; adapt, don't forward

Vendor the MIT driver binary, digest-pinned per release in `internal/computeruse/pin.go`; download on first enable to `~/.buddies/drivers/cua-driver/<version>/`, verify archive + binary digests, keep the vendor signature (notarized → no Gatekeeper friction; TCC attribution is unaffected because we never re-sign).

The adapter (`computer-mcp`) is a *curated* server, not a byte-forwarder: mcp-go server facing the parent, mcp-go stdio client facing `cua-driver mcp --socket` (the stable vendor boundary). Rationale: context weight (15 tools vs ~40), fixed-refusal discipline, and a place to hang dispatch/approval policy. Alternatives: speaking the daemon's raw socket protocol (undocumented-stable, rejected), `mcp_envelope_stream` over the socket (experimental, gated, rejected), forking the driver (rejected — kills the re-pin path).

Driver env at spawn: `DO_NOT_TRACK=1` plus the corrected `CUA_DRIVER_RS_TELEMETRY_ENABLED=0`, `CUA_DRIVER_RS_UPDATE_CHECK=0`, `CUA_DRIVER_EMBEDDED=1`, `CUA_DRIVER_PARENT_LIVENESS_STDIN=1`. Verified after boot via the driver's telemetry status output in the smoke task.

### D2 — Registry generalization with per-server opt-in

`SystemServerSpec{ID, Subcommand, DisplayName, OptIn, ConfigProvider, StatusProvider}`; the single-ref seam (`SystemServerSource`, `SystemServer()`, settings reserved-id guard, McpPane's `SYSTEM_SERVER_ID`, supervisor keys) becomes a registry slice. `browser-use` keeps `OptIn=false` (behavior bit-identical); `computer-use` ships `OptIn=true` so empty-means-all never silently grants desktop control. Alternative: materialize explicit allowlists in a migration (rejected — heavier, and wrong once more system servers arrive).

Supervision is keyed by spec ID; the supervisor mechanism itself is already generic.

### D3 — Single dispatcher in the adapter

`computer-mcp` holds one process-wide dispatch mutex: acquire per mutating action, fixed refusal on contention. This *is* the controller lease — the daemon offers none, and the adapter is the sole process that talks to it. Read-only tools (`list_apps`, `list_windows`, `observe_*`, `zoom`, `verify`) share the same lock: observation must not race an in-flight action's settle window. The busy refusal is a fixed string ("Desktop is controlled by another agent. Wait for their turn to end, then observe again.") — no holder identity, mirroring the attention refusal discipline.

### D4 — Tool surface (curated 15)

| buddies tool | driver backing | tier |
|---|---|---|
| `list_apps`, `list_windows` | same | read |
| `observe_window` | `get_window_state` | read |
| `observe_desktop` | `get_desktop_state` | read |
| `click`, `double_click`, `right_click`, `drag`, `scroll` | driver equivalents (element token or coordinate+capture) | act |
| `type`, `key`, `hotkey` | `type_text`, `press_key`, `hotkey` | act |
| `set_value` | `set_value` | act |
| `read_clipboard`, `write_clipboard` | clipboard tools | act |
| `verify` | `verify_state` | read |
| `zoom` | `zoom` | read |
| `wait` | settle/wait | read |
| `stop` | `revoke`-equivalent + local latch | safety |

Excluded for v1: `run_script`, `run_steps`, sessions/escalate (deprecated), recording/replay, agent-cursor theming, perception (AGPL), driver config tools. Element tokens are surfaced verbatim as opaque strings with fail-closed staleness; v1 clicking leans coordinate+capture and AX `set_value` — token-path regressions (#5910) are re-verified at each pin bump before adoption.

### D5 — Observation image lane: `observationModel` decorator

The decorator (sibling of `sanitizeModel`, wrapped at the same build site) rewrites only the provider's view on every `Generate`/`Stream`:

1. Scan outbound history for observation markers (`[screenshot: sessions/<sid>/computer/<ts>.png]`) not yet elided.
2. Last N=2 markers: load the PNG (path jail-checked: absolute, clean, under the session's computer dir), inject one synthesized user-role message — text part `[computer-use observation]` + `user_input_image` block (base64).
3. Older markers: rewritten to `(earlier screenshot elided)`.
4. `caps.ImageInput` false → no injection at all (catalog gate, `turninput.go` precedent).

Marker text is the persisted, replay-safe form; injection is a pure function of history (idempotent, resume-safe). No hook-appended messages — ADK state is untouched, so the react-loop breakage class (unknown-tool-call fix) cannot occur. Per-family interleaving acceptance (Claude/OpenAI/Gemini tolerating a user message between tool result and assistant) is a smoke item; all three support it structurally (it's Anthropic's native tool_result+image shape).

### D6 — Permission warm-up gate

Probes live in a new `internal/services/settings_computer.go` behind a build-tag seam (`_desktop.go` / `_server.go`, the clipboard pattern): macOS `AXIsProcessTrusted` + `CGPreflightScreenCaptureAccess` via cgo or a tiny helper; other desktop platforms report "no permission ceremony" and go straight to ready. The *prompt* is raised host-side only (`kAXTrustedCheckOptionPrompt: true` / `CGRequestScreenCaptureAccess`) from the pane's enable action — the driver never prompts (embedded mode enforces this). Compositions: `WithSystemComputerUse(...)` mirrors `WithSystemBrowserUse`; `WithoutSystemComputerUse()` for tests and server mode.

### D7 — Approval gate: shell-gate semantics at the tool boundary

`wrapApprovalTools` (new, in `attention.go`, beside `wrapAttentionTools`) intercepts by name suffix over the named surface: `kill_app`-class destruction, `write_clipboard`, and credential-field typing (flagged by the adapter in the tool result metadata the parent already parses — the adapter detects secure fields from the driver's element metadata; undetectable fields are not gated, documented limitation). Gated calls raise `KindApprovalRequired` with a payload naming tool + target; approve resumes the exact call, deny feeds the fixed refusal. No new event kinds, no new card renderer. Tiers: read-only tools ungated; `stop` never gated (it's the kill switch).

### D8 — Frontend: pane, rows, trajectory

- `ComputerUsePane.tsx`: enable toggle, permission card (per-permission status, Manage deep links via `x-apple.systempreferences:...`, Check again), download state, status line — mirroring the OpenBot surface the user validated.
- `McpPane.tsx`: `SYSTEM_SERVER_ID` → registry set; rows per system server.
- `ComputerTool.tsx` + vendored `computer-use` element copy (runtime lane): `MessagePrimitive.GroupedParts` grouping (already used by `ChatToolGroup`) keyed to consecutive `mcp__computer-use__*` parts; backdrop via new `client.chat.computerScreenshot` bound read (same `^\d+\.png$` jail over `sessions/<sid>/computer/`, `chat_screenshots.go` extended); pixel→percent coordinate conversion; token actions as footer-only steps; `type` text masked (length only); chrome restyled desktop-frame (title = app name, no URL bar).
- `toolDisplay.ts`: `COMPUTER_USE_PREFIX` sentence map.

### D9 — Platform and mode gating

Server mode: the whole lane is absent (`_server.go` seam returns unavailable; composition omits the spec). Windows: driver reports UIPI refusals for elevated targets (readable error passthrough). Linux: X11 default, Wayland opt-in env documented as limitation. Multi-display: primary only — the observe tools accept no display argument in v1.

### D10 — Seeded computer-use skill

The agent-facing playbook ships as a seeded global-tier skill (`computer-use`, `internal/computeruse/skill.go`), the knowledge-search/documents seed pattern verbatim: repair-if-deleted, never overwrite, cosmetic to startup. The global tier is right despite opt-in tools because skill eligibility is already per-agent (global enable × allowlist); the body opens with the one guard line covering agents without the server. Tool descriptions stay the local targeting contract; the skill teaches the loop — observe first, target from the latest observation, prefer `set_value`, verify outcomes, respect the approval gates, treat the busy refusal as final.

## Risks / Trade-offs

- [Vendor velocity: 4 minors in 16 days with breaking changes] → digest pin + scheduled re-pin task with a pinned-tag verification smoke; adapter isolates the driver surface to one package.
- [Daemon concurrency is our problem] → single dispatcher (D3); the adapter is the only daemon client.
- [Interleaving a user message after a tool result may confuse a connector] → per-family live smoke in tasks; decorator is one flag to disable per family if a family chokes.
- [Image tokens inflate turns] → rolling window N=2 + driver-side downscale; window size is a constant, tunable post-smoke.
- [Telemetry silently on if env names drift again] → `DO_NOT_TRACK=1` belt + suspenders with `_RS_` vars; smoke asserts no PostHog egress via the driver's status output.
- [TCC grant orphans on app update] → buddies signs consistently; the pane's Check again + daemon restart is the recovery path; troubleshooting note documents `tccutil reset`.
- [Trajectory UI leaks typed secrets] → footer masking (D8) + credential-typing approval gate (D7); args redaction asserted in the tool-cards tests.
- [Adapter crash loses the dispatch lock mid-action] → lock is in-process only; adapter respawn clears it; daemon-side `revoke` cleans stranded driver state at adapter boot.

## Migration Plan

Purely additive; no store migrations. Build order ships independently: (1) registry generalization with browser-use behavior identical, (2) adapter + driver lifecycle behind the off-by-default toggle, (3) permission gate + pane, (4) observation lane, (5) approvals + trajectory UI, (6) windows/linux bring-up. Rollback = disable the toggle; the registry refactor is behavior-preserving for browser-use either way.

## Open Questions

- Credential-field detection quality on the driver's element metadata — if unreliable, the credential-typing gate degrades to masking-only until a better signal exists (spec scenario stays satisfiable: the gate covers fields the adapter can identify).
- Whether `observe_desktop` earns its keep in v1 or ships behind the same tool flag as a later pin bump (cheap to defer; the marker contract is identical).
