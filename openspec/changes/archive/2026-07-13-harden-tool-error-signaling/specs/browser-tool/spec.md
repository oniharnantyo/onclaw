## MODIFIED Requirements

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

## ADDED Requirements

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
