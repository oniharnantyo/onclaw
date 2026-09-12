# web-app/runs Specification

## Purpose

The run history screen: every agent execution in the workspace — chat turns, cron fires, and API calls — as a filterable table.

## Requirements

### Requirement: Run history table
The runs screen SHALL list scheduler runs with identifier, agent name, trigger (`scheduler` for a scheduled fire, `manual` for run-now), start time, duration, token count, and status, with monospace treatment for identifiers, times, and counts. Filter chips SHALL narrow the list to All, Succeeded, or Failed. Each run SHALL link to its transcript; runs of a single scheduler SHALL be reachable from that scheduler's last-run cell.

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

### Requirement: Responsive run rows
Below 768px each run row SHALL reflow into a stacked card presenting the same data without horizontal overflow, rather than a clipped or scrollable grid.

#### Scenario: Mobile run row
- **WHEN** the viewport is 360px wide
- **THEN** every run renders as a stacked card with all seven data points visible and no horizontal scrollbar
