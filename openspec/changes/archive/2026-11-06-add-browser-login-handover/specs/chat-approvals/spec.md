# chat-approvals Specification (delta)

## ADDED Requirements

### Requirement: Browser attention requests

A Browser Use secret or takeover request SHALL park the turn as a tool-call attention part on the approval seam — `approval: {id, prompt, display}` with `display: "text"` for secrets and `display: "decision"` for takeover — pending while the parked tool waits. A secret card SHALL collect the value through a masked input (password field or digit boxes) — and, when the request declares an identifier target, a plain username-or-email identifier box beside it — SHALL clear its local values on submit, and SHALL submit them through the approval response; the message part and the `input_resolved` session event SHALL record a receipt only (outcome, redacted length, optional saved-login id) — never the value. A takeover card SHALL present Approve/Cancel while pending and Done/Cancel while active. A pending request that outlives the process SHALL resolve `expired` on the next load, and the agent SHALL re-request. The runtime SHALL accept a text response only when the request declares it (`display: "text"`).

#### Scenario: Secret card parks and clears

- **WHEN** the agent calls `submit_secret` and the user types the password and submits
- **THEN** the turn parks on the masked card until submission, the card clears its local value, the turn resumes, and the message part shows a receipt with no trace of the typed value

#### Scenario: Expired pending request

- **WHEN** a secret request is pending and the app restarts before the user answers
- **THEN** the card renders as expired, the turn does not resume with a stale value, and the agent may re-request

#### Scenario: Takeover card states

- **WHEN** the agent calls `request_takeover`
- **THEN** the card shows Approve/Cancel while pending, flips to Done/Cancel once the browser window is handed over, and resolves the parked tool when the user presses Done or Cancel
