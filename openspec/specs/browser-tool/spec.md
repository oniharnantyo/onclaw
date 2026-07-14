# browser-tool Specification

## Purpose
TBD - created by archiving change add-browser-tool. Update Purpose after archive.
## Requirements
### Requirement: Engine-swappable browser abstraction

The system SHALL provide a browser capability behind `Engine`, `Context`, and `Page`
interfaces such that the rendering engine is replaceable without changing the tool surface.
Because the supported engines speak the Chrome DevTools Protocol, a single CDP-backed engine
implementation SHALL serve them, and an engine SHALL be selected by providing a CDP endpoint
(WebSocket URL). The system SHALL preserve `CGO_ENABLED=0` static builds and ARM
cross-compilation.

#### Scenario: Engines are swapped without tool changes

- **WHEN** the configured engine changes from Lightpanda to Chromium
- **THEN** the browser tools behave identically without code changes

#### Scenario: The CDP driver introduces no CGO

- **WHEN** the project is built with `CGO_ENABLED=0`
- **THEN** the browser subsystem compiles and cross-compiles to ARM targets

### Requirement: Lightpanda is the default engine

The system SHALL default to Lightpanda as the browser engine, launched as a sibling process
exposing a CDP server which the system drives over the Chrome DevTools Protocol. The default
SHALL be gated on Lightpanda's CDP coverage of the accessibility tree; where coverage is
insufficient or the host architecture cannot run Lightpanda, the operator SHALL configure the
Chromium engine instead.

#### Scenario: Lightpanda is launched as a CDP server

- **WHEN** the browser engine starts with the default configuration
- **THEN** a Lightpanda process is spawned exposing CDP and the driver connects to it

#### Scenario: Unsupported architecture falls back

- **WHEN** the host cannot run the Lightpanda binary
- **THEN** the operator configures the Chromium engine instead of failing

### Requirement: Granular browser tools

The system SHALL expose the browser capability as discrete tools — at minimum navigate,
snapshot, act, screenshot, open, close, tabs, status, start, stop, and console — each
registered as an individual tool in the builtin tool registry under a single browser
category. All browser tools SHALL share one engine instance.

#### Scenario: Browser tools are individually toggleable

- **WHEN** a browser tool is disabled via the tools-management surface
- **THEN** that tool is withheld from the agent while the other browser tools remain available

### Requirement: Snapshot and act share an element-ref contract

The snapshot tool SHALL return an accessibility tree annotated with element references, and
the act tool SHALL accept those references to interact with elements. The page handle that
produced a snapshot SHALL be the one that resolves its references, so a reference is only
valid within the page that emitted it.

#### Scenario: A reference from a snapshot is clickable

- **WHEN** the agent takes a snapshot and then acts on a reference from that snapshot
- **THEN** the action targets the corresponding element on the same page

#### Scenario: A stale reference is not honored

- **WHEN** the agent uses a reference after the page has navigated away
- **THEN** the act does not target a wrong element

### Requirement: Browser configuration flows through tools-management

The browser category SHALL register a configuration schema with the tools-management
configurable-category registry, and the engine SHALL read its configuration (engine, headless
mode, and per-engine settings) from the per-category configuration store. When no
configuration is stored, the system SHALL apply code defaults.

#### Scenario: Engine settings are edited via the category config

- **WHEN** the user edits the browser category configuration in the Tools UI
- **THEN** the engine reads the new settings on its next start

#### Scenario: Defaults apply when unconfigured

- **WHEN** no browser configuration is stored
- **THEN** the engine starts with code defaults

### Requirement: Browser sessions are scoped to the engine process

In v1, a browser session (cookies and storage) SHALL persist for the lifetime of the engine
process and its browser context, enabling an agent to log in and continue within a run.
Sessions SHALL NOT persist across engine or process restarts in v1.

#### Scenario: Login state persists within a run

- **WHEN** the agent logs into a site and navigates further in the same context
- **THEN** the authenticated session is retained

#### Scenario: A restart clears the session

- **WHEN** the engine process restarts
- **THEN** prior session state is no longer present

### Requirement: Browser tools degrade safely without an engine

The system SHALL return a clear engine-unavailable result to the agent rather than crashing when the
configured browser engine is not available (e.g. the Lightpanda binary is absent) or no page is
active, and non-browser tools SHALL be unaffected. The engine-unavailable / no-active-page outcome
SHALL be returned as a tool-result observation with no fatal error so the agent turn continues (the
agent may start a browser and retry).

#### Scenario: A missing engine returns an observation

- **WHEN** a browser tool is invoked but the engine binary is not installed
- **THEN** the tool returns a human-readable engine-unavailable observation with no fatal error, and
  the agent turn continues

#### Scenario: No active page returns an observation

- **WHEN** a browser tool that requires an active page is invoked before any page is open
- **THEN** the tool returns a human-readable no-active-page observation with no fatal error, and the
  agent turn continues

### Requirement: Browser runtime failures are recoverable observations

Browser tools SHALL return runtime failures as tool-result observations with no fatal error, so the
agent turn continues and the model can recover. Runtime failures include navigation timeouts,
element references that are not found on the page, failed actions (click/type/press), and JavaScript
evaluation errors. The observation SHALL name the operation and the reason. Context cancellation
SHALL be propagated and not converted.

#### Scenario: A navigation timeout is an observation

- **WHEN** the agent calls `browser_navigate` and the navigation times out
- **THEN** the tool returns a human-readable observation naming the URL and the timeout with no
  fatal error, and the agent turn continues

#### Scenario: An element reference not found is an observation

- **WHEN** the agent calls `browser_act` with a reference that is not present on the active page
- **THEN** the tool returns a human-readable observation that the reference was not found with no
  fatal error, so the agent can re-snapshot and retry

#### Scenario: A failed action is an observation

- **WHEN** a browser action (click, type, press, hover) cannot be performed
- **THEN** the tool returns a human-readable observation naming the action and the reason with no
  fatal error, and the agent turn continues

#### Scenario: Context cancellation is propagated

- **WHEN** a browser operation is in flight and the context is cancelled or its deadline expires
- **THEN** the cancellation is returned as a fatal error and is not converted to an observation

