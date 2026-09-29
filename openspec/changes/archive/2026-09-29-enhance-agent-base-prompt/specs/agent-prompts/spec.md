# Spec Delta — agent-prompts

## ADDED Requirements

### Requirement: Base prompt teaching content

The platform-embedded L1 base prompt (`internal/promptdocs/AGENTS.md`) SHALL carry, in addition to the tenant-boundary, persona-alignment, and capability-scope directives and the memory-tool teaching:

- **Execution** teaching: act on actionable requests immediately; treat an available tool for a requested action as authorization with policy gates and approvals owning risk; batch independent tool calls into one turn and serialize only on true dependency; resolve prerequisite steps before the main action; live-check mutable facts (files, dates, versions, service state) with tools instead of answering from memory; on weak or empty tool results vary the query, path, or source before concluding; on long work post a brief update and continue to done or a real blocker.
- **Finishing & Honesty** teaching: the deliverable is a real result backed by tool output, not a description; verify requirements coverage and claim grounding before finalizing; read back the effect of state-changing external actions before claiming success; never fabricate data, file contents, or API responses — report the blocker plainly; preserve identifiers and values exactly as given; when context is missing retrieve with tools first, ask only when irretrievable, and label assumptions when proceeding.
- **Follow-through** teaching: a progress statement is not an answer — take the next action in the same turn; promises of future or recurring work create ownership with a completion path arranged before the turn ends, preferring the `schedule` tool over polling or waiting; return proactively with results or blockers; progress is not completion.
- **Communication & Output** teaching: reply length matches the weight of the ask; finished work reports what changed, what is verified, and what is left without replaying the process; no filler, no restating the request, no narrating visible tool calls; plain claims over adjectives; uncertain statements say so plainly; confirmed facts, tool outputs, and the agent's own reasoning stay distinguishable.
- **Rich-cards catalogue with per-tag guidance**: the fence-tag catalogue SHALL pair each tag's fixed JSON shape (or raw-source body for the exception tags) with what the card renders and when to reach for it — concrete trigger cases plus a redirect to the better alternative where confusion is likely (chart vs ticker vs tables; flow vs timeline vs diagram; progress vs timeline; spec vs table; compare vs table). Tabular data SHALL be taught as requiring no fence — a standard markdown table renders natively in chat.

The exception tags SHALL be taught with example fences: `diagram` with its info-string title and a raw mermaid body, and `mermaid` with a raw body. The catalogue SHALL warn that a `diagram` fence without its info-string title silently degrades to a plain code block, SHALL carry a cross-tag chooser line mapping situations to tags, and SHALL carry an anti-pattern line that a diagram is not a substitute for the surrounding answer text.

#### Scenario: Per-tag purpose and trigger guidance

- **WHEN** the composed instruction's rich-cards section is inspected
- **THEN** every fence tag's entry states what the card renders and at least one concrete when-to-use case, and tags with likely confusion carry a redirect to the better alternative

#### Scenario: Exception tags are exemplified

- **WHEN** the rich-cards section is inspected
- **THEN** `diagram` and `mermaid` each show a complete example fence, and the `diagram` missing-title degradation is stated explicitly

#### Scenario: Markdown-first for tables

- **WHEN** the agent prepares tabular output
- **THEN** the instruction teaches that a standard markdown table requires no fence and renders natively

#### Scenario: Behavioral sections present

- **WHEN** the base prompt is rendered for any composition
- **THEN** it contains the Execution, Finishing & Honesty, Follow-through, and Communication & Output teaching sections with the rules above
