## ADDED Requirements

### Requirement: Web tool failures are recoverable observations

`web_fetch` and `web_search` SHALL return a fetch or search failure as a tool-result observation
with no fatal error, so the agent turn continues and the model can recover (retry, try another URL,
or report to the user). This applies both when the configured provider fails (already covered by the
fallback notice) and in the terminal case where the default fallback provider also fails. The
observation SHALL name the requested URL or query and the reason. Context cancellation SHALL be
propagated and not converted.

#### Scenario: A fetch failure is an observation

- **WHEN** the agent calls `web_fetch` and every available fetch provider fails (e.g. the URL is
  unreachable or returns an error status)
- **THEN** the tool returns a human-readable observation naming the URL and the failure reason with
  no fatal error, and the agent turn continues

#### Scenario: A search failure is an observation

- **WHEN** the agent calls `web_search` and every available search provider fails (e.g. network
  error or rate limit)
- **THEN** the tool returns a human-readable observation naming the query and the failure reason
  with no fatal error, and the agent turn continues

#### Scenario: A successful fetch still returns content

- **WHEN** the agent calls `web_fetch` and a provider succeeds (directly or via fallback)
- **THEN** the tool returns the fetched content as before, with any fallback notice prepended

#### Scenario: Context cancellation is propagated

- **WHEN** a fetch or search is in flight and the context is cancelled or its deadline expires
- **THEN** the cancellation is returned as a fatal error and is not converted to an observation
