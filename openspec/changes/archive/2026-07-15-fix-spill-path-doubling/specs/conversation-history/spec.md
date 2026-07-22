## MODIFIED Requirements

### Requirement: Summarization preserves spilled-result file paths

The summarizer-input scrub SHALL treat a tool result that carries a spilled-artifact path (an
envelope produced by the file-reference spill mode) as a path-bearing result, analogous to a
file-producing tool. When condensing such a result, the scrub SHALL inspect the result content for a
path matching the session-scoped spill shape (`sessions/<session_id>/tool_results/…`), preserve that
path verbatim in the stub, and drop the envelope preview/body. This extends the existing
durable-pointer preservation so that large results offloaded to session-scoped files remain
recallable by path after compaction, rather than being reduced to a generic `[cleared <tool>]` stub
that discards the path.

#### Scenario: A spilled result is summarized by its spill path

- **WHEN** the compacted range contains a tool result whose envelope references a file under `sessions/<session_id>/tool_results/`
- **THEN** the summarizer input references the spill path verbatim and does not include the envelope preview or the spilled file's content

#### Scenario: The agent can recall a spilled artifact after compaction

- **WHEN** a spilled result has been compacted to a path-preserving stub and the agent calls `read_file` with the preserved spill path on a later turn
- **THEN** the spilled file's contents are returned

#### Scenario: A non-spilled result of the same tool is still cleared generically

- **WHEN** the compacted range contains a `web_fetch` result that is not a spilled envelope (no tool_results path)
- **THEN** the result is condensed to the generic `[cleared web_fetch]` stub as before
