# Proposal: always-on-channel-tools

## Why

The three channel tools (`channel.post`, `channel.history`, `session.close`) are context-granted at runtime — the runner appends them for channel runs and strips them everywhere else — but every config surface pretends they are ordinary tools. Selecting them in the agent config is a silent no-op, a workspace admin can toggle them off in Settings → Tools and thereby strip them from live channel runs (breaking the channel with no way for agents to post or read history), and their icons render blank because the catalog's declared icon keys (`message`, `history`, `check-circle`) have no glyphs in the frontend icon set.

## What Changes

- The tool catalog gains a **non-toggleable (always-on)** notion. The three channel tools carry it; every other tool stays toggleable. The flag is surfaced in the tools API payload (`toggleable: false`) so clients derive behavior from data, not hardcoded keys.
- **BREAKING**: `PATCH /tools/:key` with `enabled` on a non-toggleable tool returns 422 (previously accepted and stored a row that silently stripped the tool at runtime).
- The workspace tool gate **exempts non-toggleable tools**: stored `enabled=false` rows (including stale rows written before this change) can no longer strip them from channel runs. No migration — old rows are simply ignored.
- The Tools settings pane groups the three into a **"Channel Tool" section** (at the top of the pane) whose rows show an always-on badge instead of an enable toggle — "Always on · channel runs" for post/history, "Always on · facilitator only" for session close. The rest of the pane stays the flat list it is today.
- The agent config Built-in Tools picker **excludes non-toggleable tools** (15 chips instead of 18).
- The frontend icon set gains the three missing glyphs (`message`, `history`, `check-circle`), and the static name/icon mirror in `toolCatalog.ts` gains the three channel tools so transcript cards resolve name + icon before the catalog fetch.
- Explicit non-goal: channel tool cards still carry no one-liner sentence in the human-readable card layer (the sentence table predates the channel tools); that stays deferred.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `workspace-tools`: the catalog carries non-toggleable always-on entries; the tools API exposes the flag and rejects `enabled` patches on them (422); the workspace gate exempts them from the enabled set.
- `agent-channels`: `channel.post` and `channel.history` exposure in channel runs SHALL NOT be withdrawable via workspace tool settings — always active for the run's lifetime.
- `channel-teams`: `session.close` exposure (facilitator, active work session) SHALL NOT be withdrawable via workspace tool settings.
- `web-app/settings`: the Tools pane renders a "Channel Tool" group section with always-on badges replacing toggles for the three; the remainder stays a flat list.
- `web-app/agents`: the Step 3 / Capabilities tool chips exclude non-toggleable catalog tools.

## Impact

- **Backend**: `internal/agents/tool_catalog.go` (flag + entries), `internal/agents/tool_gate.go` (exemption), `internal/server/handlers/tools.go` (payload + PATCH guard); `internal/agents/runner.go` comments already describe the scoping contract and stay.
- **Frontend**: `web/src/components/ui/Icon.tsx` (three glyphs), `web/src/lib/toolCatalog.ts` (static mirror +3), `web/src/screens/settings/ToolsPane.tsx` (group section + badge + no toggle), `web/src/modals/AgentConfigModal.tsx` (picker filter).
- **Tests**: catalog/gate/handler unit tests, ToolsPane and AgentConfigModal tests, the session-tools catalog-group pin (`session_tools_test.go`), smoke.sh tool-settings coverage if it toggles a channel tool.
- **Stored state**: no migration. Stale `workspace_tool_settings` rows for the three keys become inert (gate ignores them); stale keys inside agent `tools` arrays keep working (runtime strips/scopes them as before).
