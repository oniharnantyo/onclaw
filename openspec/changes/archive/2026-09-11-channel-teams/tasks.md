# Tasks: channel-teams

## 1. Domain & migrations

### 1.1 Work session domain
- [x] `domain.WorkSession` (ID, WorkspaceID, ChannelID, RootMessageID, Goal, Status, PauseReason, Budget, HopsUsed, Summary, timestamps) with lifecycle transitions (`open→paused→open`, `*→closed`) as validated state changes
- [x] `domain.ChannelMember.Role` (`member|facilitator`) + single-facilitator sentinel error
- [x] Kickoff flag + `WorkSessionID` on `domain.ChannelMessage`

### 1.2 Migration
- [x] Migration `000040_channel_teams`: `channel_members.role`, `channel_work_sessions`, `channel_messages.work_session_id` + `is_kickoff`, facilitator partial unique index — per design D7
- [x] Bump `latestSchemaVersion`; down migration reverses cleanly

## 2. Store layer

- [x] `WorkSessionStore` (port + fake + postgres): create, get, update transitions with optimistic status checks (pause/resume/close races), list-by-channel
- [x] Membership role read/write; facilitator uniqueness enforced in store
- [x] Message writes accept `work_session_id` / `is_kickoff`; integration tests (`-tags=integration`)

## 3. Chokepoint session branch

- [x] Kickoff handling: flagged post mints the session and summons the facilitator (session context in `ExecRequest`)
- [x] Session-aware bounds: in-session mentions summon deterministically against hop budget; v1 chain caps suspended; silence tiers still govern untagged in-session messages
- [x] Hop accounting with pause on exhaustion (facilitator status summons); resume on human post; suppress summons on closed sessions
- [x] Human gate: agent-tagged-human in session → `awaiting-human` pause + awaiting-state event
- [x] Stall watchdog: idle timer per open session → one facilitator summons per idle period

## 4. Facilitator tooling

- [x] `session.close` registry tool (summary arg) — exposed only to the facilitator agent; stores summary, closes session, posts via chokepoint
- [x] Session context block in the CHANNEL.md channel branch (goal, status, hops remaining)

## 5. Shared project space

- [x] Project directory lifecycle: created with channel (slug-named), removed with workspace cascade
- [x] Jail extension: mount `/project` read-write for member agents only; escape/symlink rules verified against the new root
- [x] Tests: member write/read, non-member absence, symlink escape rejection

## 6. Team templates

- [x] Built-in "Software Team" definition (role slots + specializations + conventions prefill + facilitator slot)
- [x] Materialization service: create channel + memberships; slot binding to spawned (role-informed promptgen identity) or existing agents
- [x] Template listing endpoint (built-ins only)

## 7. HTTP API

- [x] Kickoff flag on message post; session create handled server-side
- [x] Session read endpoints: per-channel list + detail (status, hops, summary)
- [x] Awaiting-state surfaces in channel/mention state payloads

## 8. Web UI (GATED — see 8.0)

### 8.0 ASCII gallery — HARD GATE
- [x] Draw full-surface ASCII gallery: kickoff affordance in composer, session banner states (open + hops, paused×2, closed + summary), awaiting-human presentation, template materialization flow
- [ ] User approval; bind approved gallery into design.md
- [ ] No UI implementation before approval

### 8.1 Session surfaces
- [ ] Kickoff affordance in the composer; kickoff messages visually marked in the feed
- [ ] Session banner with live status; awaiting-human channel state; closed summary rendering

## 9. Verification

- [x] `go build ./...`, `go vet ./...`, `go test ./...` green; integration tests for new tables
- [x] Smoke: materialize Software Team → kickoff → in-session multi-hop chain → human gate pause → resume → facilitator close
- [x] Budget/watchdog unit tests (timer-driven paths)
- [ ] `openspec validate --strict` clean; manual browser pass left open as the final task
