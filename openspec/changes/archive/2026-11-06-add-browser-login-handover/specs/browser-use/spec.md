# browser-use Specification (delta)

## ADDED Requirements

### Requirement: Secure secret submission

The system server SHALL expose `submit_secret` whose input schema carries field targets and submission controls only — `targets`, `method` (`password|otp|authenticator`), `digits`, `submission` (`click|enter|on_input`), optional `submitTarget`, `identifierTarget`, `loginId`, `username` — and SHALL reject any value-bearing field. An `identifierTarget` names the login form's identifier field (username or email): the handover card then also collects an identifier, which the server SHALL fill before the secret and fingerprint exactly like every other target. Before showing a request to the user the server SHALL validate: HTTPS-only origin, no framed targets, no request already pending on the session, isolation from connected same-site tabs, and it SHALL fingerprint the form and re-check the fingerprint immediately before entry. The user's value and typed identifier SHALL be delivered only over the parent-initiated internal channel, SHALL be typed via trusted CDP input events, and SHALL never appear in tool arguments, tool results, snapshots, screenshots, session events, the message transcript, or logs: the tool result SHALL be a fixed-shape receipt (`status`, optional `savedAsLogin`), error paths SHALL return fixed reason codes, and inspection tools on the session SHALL be refused from request until scrub-release. After submission the server SHALL clear filled fields and scan the document (title, URL, text, values, attributes, open shadow roots): a clean same-document page MAY stay open with `evaluate` and recording blocked until a real main-frame navigation (which clears history); a page where the value is findable SHALL be reloaded with GET. Navigation whose URL carries the secret SHALL be blocked. Secrets SHALL NOT be retried. When saving to the vault fails, sign-in SHALL still proceed.

#### Scenario: Model cannot supply or observe a value

- **WHEN** a turn issues `submit_secret` with a value field, or any tool attempts snapshot/screenshot/read/evaluate on a session between request and scrub-release
- **THEN** the value field is rejected by schema, and the inspection tools answer with the protection refusal

#### Scenario: Fill, scrub, and receipt

- **WHEN** the user submits a password on a same-document sign-in page and the scrub scan finds the value nowhere
- **THEN** the fields are cleared, the page stays open with evaluate/recording blocked until the next real navigation, and the tool result is `{"status":"completed"}` with no value-bearing content anywhere in the session's events or transcript

#### Scenario: Echo falls back to reload

- **WHEN** the scrub scan finds the typed value echoed in a shadow-root text node
- **THEN** the page is reloaded with GET and the value appears in no snapshot or log

### Requirement: User takeover

The system server SHALL expose `request_takeover` (reason `sign-in-blocked`) which parks the calling turn on an attention card with Approve and Cancel. Approving SHALL relaunch the profile's browser headed and focused, invalidate references and snapshots, and hold the session: every tool on it SHALL return a fixed refusal until the user presses Done (resolving the request `completed` with "take a fresh snapshot") or Cancel (`cancelled`). The held state SHALL end only at resolution or tab close, with no timeout. A takeover card SHALL be shown only after the handover has taken effect.

#### Scenario: Agent blocked while the user signs in

- **WHEN** the user approves a takeover and signs in by hand in the headed window
- **THEN** every agent tool on that session returns the fixed refusal until Done is pressed

#### Scenario: Resolution hands the page back

- **WHEN** the user presses Done after signing in
- **THEN** the held state ends, the parked tool returns `{"status":"completed","next":"take a fresh snapshot"}`, and the agent re-inspects the page before acting
