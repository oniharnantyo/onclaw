# Capability: web-app/runs

## Purpose

The run history screen: every agent execution in the workspace — chat turns, cron fires, and API calls — as a filterable table.

## ADDED Requirements

### Requirement: Run history table
The runs screen SHALL list runs with identifier, agent name, trigger (chat, cron, manual, api), start time, duration, token count, and status, with monospace treatment for identifiers, times, and counts. Filter chips SHALL narrow the list to All, Succeeded, or Failed.

#### Scenario: Filter to failures
- **WHEN** the user selects the "Failed" filter
- **THEN** only failed runs are listed, with failed status rendered in the danger color

#### Scenario: No runs at all
- **WHEN** the workspace has no runs
- **THEN** the table shows "No runs yet — chat with an agent or fire a schedule."

#### Scenario: Filter matches nothing
- **WHEN** a filter is active and no runs match it
- **THEN** the table shows "No runs match this filter."

### Requirement: Responsive run rows
Below 768px each run row SHALL reflow into a stacked card presenting the same data without horizontal overflow, rather than a clipped or scrollable grid.

#### Scenario: Mobile run row
- **WHEN** the viewport is 360px wide
- **THEN** every run renders as a stacked card with all seven data points visible and no horizontal scrollbar
