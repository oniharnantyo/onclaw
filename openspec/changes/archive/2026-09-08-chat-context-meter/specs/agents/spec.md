## ADDED Requirements

### Requirement: Effective context exposure
The agents API SHALL expose, on the agent payload, two computed read-only fields describing the context budget execution actually enforces: `effective_context_window` — the agent's resolved context window (its stored `context_window`, or the 200,000-token default when none is stored) — and `summarization_trigger_tokens` — the effective window multiplied by the server's summarization margin. The values SHALL be derived at read time from the same resolution execution applies, and clients SHALL treat them as authoritative display data rather than recomputing resolution or duplicating the margin constant. These fields SHALL NOT be writable through create or update requests.

#### Scenario: Stored window is echoed as effective
- **WHEN** an agent has a stored `context_window` of 50,000
- **THEN** its payload reports `effective_context_window` 50,000 and `summarization_trigger_tokens` 37,500 with a 0.75 server margin

#### Scenario: Unset window falls back to the default
- **WHEN** an agent has no stored `context_window`
- **THEN** its payload reports `effective_context_window` 200,000

#### Scenario: Not writable
- **WHEN** a create or update request supplies `effective_context_window` or `summarization_trigger_tokens`
- **THEN** the request ignores those fields and the stored agent is unaffected
