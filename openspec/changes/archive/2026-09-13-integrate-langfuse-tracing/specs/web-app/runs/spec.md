## MODIFIED Requirements

### Requirement: Run history table
The runs screen SHALL list scheduler runs with identifier, agent name, trigger (`scheduler` for a scheduled fire, `manual` for run-now), start time, duration, token count, and status, with monospace treatment for identifiers, times, and counts. Filter chips SHALL narrow the list to All, Succeeded, or Failed. Each run SHALL link to its transcript; runs of a single scheduler SHALL be reachable from that scheduler's last-run cell. A run carrying a persisted observability trace id SHALL additionally offer an "Open in Langfuse" action targeting that trace on the configured host; runs without a trace id render no such action.

#### Scenario: Filter to failures
- **WHEN** the user selects the "Failed" filter
- **THEN** only failed runs are listed, with failed status rendered in the danger color

#### Scenario: No runs at all
- **WHEN** the workspace has no scheduler runs
- **THEN** the table shows an empty state inviting the user to create a schedule

#### Scenario: Filter matches nothing
- **WHEN** a filter is active and no runs match it
- **THEN** the table shows "No runs match this filter."

#### Scenario: Run opens its transcript
- **WHEN** the user opens a run from the table
- **THEN** the run's session transcript renders — the task prompt, tool-call cards, and the final reply

#### Scenario: Traced run offers Langfuse link
- **WHEN** the user opens a run whose record carries a trace id
- **THEN** an "Open in Langfuse" action targets that trace on the configured host

#### Scenario: Untraced run has no link
- **WHEN** the user opens a run without a trace id
- **THEN** no Langfuse action is rendered
