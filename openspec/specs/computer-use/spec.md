# Computer Use Specification

## Purpose

Computer Use is buddies' system MCP server for desktop control: a supervised adapter process that drives a vendored OS-automation daemon so agents can observe and operate native applications — windows, menus, keyboard, mouse — on the machine buddies runs on. Sibling of Browser Use; the two pipelines stay disjoint.

## Requirements

### Requirement: Driver acquisition and integrity

The OS-automation driver SHALL be a vendored external binary, pinned per release with a SHA-256 digest recorded in the app. On first enable the app SHALL download the pinned release into the app's drivers directory, verify the archive and binary digests before first use, and refuse to run the driver when verification fails. The vendor's code signature SHALL be preserved (the app MUST NOT re-sign the binary).

#### Scenario: First enable downloads and verifies

- **WHEN** the user enables Computer Use for the first time
- **THEN** the pinned driver release is downloaded, its digest is verified, and the pane reports the driver as ready

#### Scenario: Digest mismatch refuses to run

- **WHEN** the downloaded binary's digest does not match the pin
- **THEN** the driver is not launched, the pane reports the integrity failure, and no computer-use tools are offered

### Requirement: Driver daemon lifecycle

When Computer Use is enabled and permissions are ready, the app SHALL spawn the driver daemon as a direct child process of the app (never through a system launcher) with a private local socket, and SHALL disable the driver's telemetry and update checks by environment. The daemon SHALL NOT outlive the app: when the app exits the daemon is gone. The app SHALL restart the daemon after an operating-system permission grant changes, because permission answers are cached per process. The app SHALL reconcile the daemon: whenever the warm-up gate answers ready but no live daemon is running — a gate probe that raced the launch, or a daemon that died later in the session — the app SHALL bring a fresh daemon up, so a daemon-less lane does not persist while the gate reads ready.

#### Scenario: Daemon spawned as direct child

- **WHEN** Computer Use is enabled and both permissions are granted
- **THEN** the daemon runs as a child of the app process on a private socket, with telemetry and update checks disabled

#### Scenario: Grant change restarts the daemon

- **WHEN** the user grants a previously missing permission and re-checks
- **THEN** the daemon is restarted before the ready state is reported

#### Scenario: A dead or never-spawned daemon self-heals

- **WHEN** the warm-up gate answers ready but no live daemon is running — the launch probe raced a grant binding, or the daemon died mid-session
- **THEN** the app spawns a fresh daemon within the reconciliation interval and tool calls recover without an app restart

#### Scenario: App exit leaves no orphan

- **WHEN** the app closes
- **THEN** the daemon and adapter processes have exited and no computer-use process remains

### Requirement: Permission warm-up gate

The app SHALL probe the platform's automation permissions (on macOS: Accessibility and Screen Recording) without raising a system prompt, and SHALL surface per-permission status with an action opening the relevant system settings pane and a re-check action. Until every required permission is granted, the computer-use server SHALL offer zero tools and the daemon SHALL NOT run. The driver itself MUST NOT raise permission prompts; the app owns prompting and attribution.

#### Scenario: Ungranted install sees no tools

- **WHEN** Computer Use is enabled but a required permission is not granted
- **THEN** the daemon is not running, every agent's turns see no `mcp__computer-use__*` tools, and the settings pane shows the missing permission with its Manage action

#### Scenario: Grant completes the warm-up

- **WHEN** the user grants the missing permissions in system settings and re-checks
- **THEN** the daemon starts and the pane reports ready

### Requirement: Adapter server process

Computer Use SHALL run as its own MCP server process spawned from the app's own binary over standard input and output, supervised like the Browser Use server: respawn with increasing backoff, a bounded consecutive-failure count that stops respawning and surfaces a failed state with the reason, and exit when the input stream closes.

#### Scenario: Adapter crash is contained

- **WHEN** the adapter process dies while an action is in flight
- **THEN** the calling turn receives an error result, the app keeps running, and the adapter is respawned with backoff

### Requirement: Curated tool surface

The server SHALL expose a fixed set of desktop tools covering: listing running apps and windows; observing a window or the whole desktop (accessibility tree text plus a screenshot); pointer actions (click, double-click, right-click, drag, scroll); keyboard actions (type text, press key, hotkey); setting an element's value directly; reading and writing the clipboard; verifying a stated condition against the live screen; zooming into a region of the latest capture; waiting for the interface to settle; and a stop action that cancels in-flight work and releases control. Each tool SHALL carry a description teaching its targeting contract. Action failures — stale targets, unknown apps, timeouts, permission gaps — SHALL return readable tool errors and MUST NOT fail the turn.

#### Scenario: Observation returns tree and screenshot

- **WHEN** the model observes a window of a running text editor
- **THEN** the result carries the window's accessibility tree as text with addressable elements, and a screenshot written to disk with its path and pixel dimensions

#### Scenario: Action failure is readable

- **WHEN** the model clicks a target that no longer exists after the window changed
- **THEN** the tool returns a readable stale-target error naming re-observation, and the turn continues

### Requirement: Single-dispatcher arbitration

The server SHALL execute desktop actions one at a time across the whole app. While an action is in flight, a second caller SHALL receive a fixed, non-retryable busy refusal naming that the desktop is controlled by another agent; it MUST NOT queue silently and MUST NOT interleave with the in-flight action.

#### Scenario: Concurrent group member is refused

- **WHEN** two agent-group members hold computer-use tools and the second acts while the first is mid-action
- **THEN** the second receives the fixed busy refusal and its turn continues

### Requirement: Observation screenshots and the marker contract

Observation and zoom results SHALL write their PNG into the calling session's workspace screenshots directory when the call's `_meta` names one — `sessions/<session id>/computer/` under the calling agent's workspace, inside the read_file jail — otherwise under the computer-use screenshots directory, and SHALL return the path and pixel dimensions. A screenshot-bearing result SHALL carry a marker referencing the written file so the runtime can deliver the pixels to vision-capable models.

#### Scenario: Screenshot lands in the session workspace

- **WHEN** the parent stamps the calling session's computer screenshots directory in the call's `_meta` (the model neither sees nor influences the value)
- **THEN** the PNG is written into `sessions/<session id>/computer/` and the result carries its path, pixel dimensions, and marker

### Requirement: Observation image delivery

When the calling model supports image input, the runtime SHALL deliver the pixels of the most recent observation markers as image content alongside the conversation, eliding older markers' images while keeping their text. When the model does not support image input, no pixels are delivered and the text tree remains the sole observation channel. The persisted transcript SHALL keep marker text only.

#### Scenario: Vision model sees the latest frame

- **WHEN** a computer-use action sequence runs on a model with image input
- **THEN** the model receives the most recent observations' images and earlier markers appear elided in the outbound view

#### Scenario: Text-only model keeps the tree

- **WHEN** the same sequence runs on a model without image input
- **THEN** no image content is injected and the accessibility tree text remains the observation channel

### Requirement: Targeting and staleness

Element addressing SHALL be valid from the latest observation; a target that no longer resolves SHALL be a readable error guiding re-observation, never a silent retry against a neighbor. Coordinate actions SHALL be bound to a fresh capture of the observed screen; a coordinate action against a stale capture SHALL be refused. Refusal and error texts SHALL be fixed strings that carry no system state beyond the target's own identity.

#### Scenario: Stale capture is refused

- **WHEN** the model clicks by coordinates taken from an observation whose screen state has since changed
- **THEN** the action is refused with the stale-capture error and the model re-observes

### Requirement: Configuration and spawn boundaries

Computer Use configuration (enabled, and any adapter knobs) SHALL persist immediately and take effect at spawn boundaries: disabling SHALL stop the daemon and adapter so no tools are offered from the next turn on, and changes MUST NOT restart a process mid-action.

#### Scenario: Disable stops the surface

- **WHEN** the user turns Computer Use off
- **THEN** the daemon and adapter stop, the MCP pane's row reports the off state, and no agent is offered computer-use tools

### Requirement: Desktop-only availability

Computer Use SHALL be inactive in server mode (no GUI session): no daemon, no adapter, no tools, and the settings pane SHALL present the capability as unavailable rather than offering toggles that cannot work.

#### Scenario: Server mode hides the lane

- **WHEN** the app runs in server mode
- **THEN** no computer-use processes run and no agent is offered computer-use tools

### Requirement: Seeded agent skill

The capability SHALL ship its agent-facing playbook as a seeded skill in the global skills tier, following the repair-if-deleted seed pattern: an existing file is never overwritten and seeding is cosmetic to startup — logged, not fatal. The playbook SHALL teach the observe-act loop — observing before acting, targeting from the latest observation, preferring direct value setting over typing, verifying outcomes — and SHALL state that the approval-gated subset is never to be routed around and that the busy refusal is final.

#### Scenario: Seed does not clobber

- **WHEN** a computer-use skill already exists in the global skills tier at startup
- **THEN** seeding writes nothing and the existing playbook stands
