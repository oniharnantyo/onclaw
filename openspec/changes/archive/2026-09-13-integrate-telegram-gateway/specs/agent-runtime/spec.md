## MODIFIED Requirements

### Requirement: Durable session index and per-user session listing

The runtime SHALL maintain a durable index of agent chat sessions so that the set of existing sessions — not just their transcripts — survives the browser. Every persistent (non-ephemeral) execution SHALL register its session in the index at run start, scoped by workspace, agent, and owning user, and SHALL bump the session's last-activity timestamp on every subsequent turn so the session surfaces as most-recently-active. The first turn of a session SHALL record a title derived from the user's input (first line, trimmed, truncated with an ellipsis); later turns SHALL never rewrite the title, and empty input SHALL leave the title unset. Ephemeral executions SHALL NOT touch the index. Gateway direct-message sessions SHALL register under the paired member exactly like web sessions, carrying a Telegram origin indicator. Gateway group sessions (origin `telegram` with a group chat binding) SHALL NOT be registered in the per-user index and SHALL NOT appear in per-user listings — they are shared sessions, accessible by direct session id and through their transcript events, mirroring the scheduler-session exclusion. The agents API SHALL expose a session listing for an agent that returns only the requesting user's non-deleted sessions for that workspace, ordered by last activity (newest first), each row carrying the session id, title, birth time, last-activity time, an origin indicator, and a running flag reflecting whether an execution is currently live for that session. Deletion SHALL be soft: the session disappears from listings while its transcript events and checkpoints remain on disk. Listing and deletion SHALL be permission-gated like the existing session-event reads; direct session access by id (transcript hydration, run binding) SHALL remain workspace-permission-scoped and SHALL NOT gain user-ownership enforcement. Sessions that predate the index SHALL NOT be backfilled — the index has no historical agent attribution to draw on.

#### Scenario: First turn births the index row
- **WHEN** a persistent turn starts on a session id that has no index row in the workspace
- **THEN** a row is created carrying the workspace, agent, requesting user, the input-derived title, and the turn time as both birth and last activity

#### Scenario: Later turns bump activity without retitling
- **WHEN** a subsequent turn runs on an indexed session whose title is already set
- **THEN** the row's last-activity time is updated, the title is unchanged, and the session orders first in a listing by last activity

#### Scenario: Compact turn does not title
- **WHEN** a compaction turn runs as the first indexed contact for a session
- **THEN** the compaction focus text never becomes the title and the row's title remains unset

#### Scenario: Listing is private per user
- **WHEN** a workspace member lists an agent's sessions
- **THEN** only sessions they own in that workspace are returned, ordered by last activity, and a session live with a run carries the running flag

#### Scenario: Telegram DM session registers with origin
- **WHEN** a paired member's gateway direct-message turn starts on a `tg_dm_` session
- **THEN** the index row is created or bumped under that member with a Telegram origin indicator

#### Scenario: Gateway group session stays out of the index
- **WHEN** a gateway turn starts on a `tg_group_` shared session
- **THEN** no per-user index row is created or bumped and the session does not appear in any member's listing

#### Scenario: Soft delete hides but preserves
- **WHEN** the user deletes one of their sessions
- **THEN** the session disappears from subsequent listings, while its persisted transcript events remain retrievable by direct id

#### Scenario: Ephemeral runs skip the index
- **WHEN** an execution runs with no session binding (ephemeral)
- **THEN** no index row is created and no existing row is touched
