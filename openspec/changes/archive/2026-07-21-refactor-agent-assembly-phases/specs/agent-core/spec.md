## ADDED Requirements

### Requirement: Agent assembly enforces a fixed handler-chain order

The assembled agent's middleware handler chain SHALL be ordered, outermost to innermost:
input-safety, summarization, history, then the filesystem group (filesystem middleware, the
filesystem toggle, and the filesystem error-recovery middleware), followed — when their
respective preconditions hold — by memory, skill, and hooks. The chain order SHALL be a
runtime property independent of the order in which the middleware are constructed: an
assembler MAY construct middleware in any order (for example, building the memory middleware
before the summarization callback, so the callback closes over an already-assigned memory
middleware) as long as the final handler slice preserves the order above.

#### Scenario: The assembled handler chain follows the documented order

- **WHEN** an agent is assembled with memory, skill, and hooks all enabled
- **THEN** the handler chain is ordered input-safety, summarization, history, filesystem, filesystem-toggle, filesystem-error, memory, skill, hooks

#### Scenario: Construction order may differ from chain order

- **WHEN** the assembler constructs the memory middleware before the summarization middleware's compaction callback
- **THEN** the callback closes over the already-assigned memory middleware, and the assembled handler chain still places summarization before memory

### Requirement: Agent assembly rejects an over-limit input floor before any model call

The assembler SHALL estimate the token cost of the fixed input floor — the system instruction
plus the assembled tool schemas — and SHALL fail assembly when that floor meets or exceeds the
context-window safety limit, returning the input-floor error and constructing no agent and
invoking no model. This gate SHALL run before the summarization middleware is built, so an
oversized static tool set is rejected at assembly time rather than mid-turn.

#### Scenario: An oversized tool set fails assembly

- **WHEN** an agent is assembled whose fixed input floor (instruction + tool schemas) meets or exceeds the context-window safety limit
- **THEN** assembly fails with the input-floor error and no agent is constructed

#### Scenario: The floor gate precedes the summarization middleware

- **WHEN** assembly reaches the input-floor check
- **THEN** the check runs before the summarization middleware is constructed, so no model call can occur for an over-limit floor
