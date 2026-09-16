## MODIFIED Requirements

### Requirement: Hook event catalog
The system SHALL support exactly five hook events in v1: `run_started` (observational), `user_prompt_submit` (blocking), `pre_tool_use` (blocking), `post_tool_use` (observational), and `run_finished` (observational). `run_finished` SHALL fire on every terminal outcome and carry a `status` value of `completed`, `failed`, or `cancelled`. Every event payload SHALL carry an `origin` value of `user`, `scheduler`, `channel`, or `heartbeat` identifying what triggered the run. Event payloads SHALL identify the workspace, agent, session, originating user, and — for tool events — the tool name, call id, and arguments.

#### Scenario: Run finished covers failure
- **WHEN** a run ends because the model provider returned an error
- **THEN** `run_finished` hooks fire once with `status` of `failed`

#### Scenario: Cron runs are gated
- **WHEN** a scheduler-triggered run submits its prompt and a `user_prompt_submit` hook applies to origin `scheduler`
- **THEN** the hook evaluates before the model is called

#### Scenario: Heartbeat runs are gated
- **WHEN** a heartbeat tick submits its checklist and a `user_prompt_submit` hook applies to origin `heartbeat`
- **THEN** the hook evaluates before the model is called, and a block ends the tick as blocked without counting toward the heartbeat's failure streak
