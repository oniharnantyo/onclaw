## Purpose

Correlates user-visible failures with server logs in self-hosted deployments: the API issues a per-request identifier on every response and includes it in error envelopes, so a value shown in an error chip can be grepped in server logs.

## ADDED Requirements

### Requirement: Request id issuance
The server SHALL issue a unique request id for every API request and return it in an `X-Request-ID` response header on success and failure alike. The id SHALL be suitable for log correlation (unique per request; opaque — no semantic meaning).

#### Scenario: Header on every response
- **WHEN** the same endpoint returns a 200 and later a 500
- **THEN** both responses carry a non-empty `X-Request-ID` header, and the two ids differ between requests

#### Scenario: Chip is greppable
- **WHEN** an operator greps server logs for the id shown in an error chip
- **THEN** the matching log line identifies the failing request (same id in the log record)

### Requirement: Request id in the error envelope
Every API error envelope SHALL include the request id alongside `code` and `message` (e.g. `error.request_id`), so clients can surface it without reading headers. Successful responses keep the current body shape (no new fields in the success payload).

#### Scenario: Envelope field matches header
- **WHEN** any endpoint returns an error envelope
- **THEN** the envelope carries a non-empty `request_id` that equals the response's `X-Request-ID` header value

#### Scenario: Success responses unchanged
- **WHEN** an endpoint returns success
- **THEN** the response body shape is unchanged — the request id appears only in the `X-Request-ID` header
