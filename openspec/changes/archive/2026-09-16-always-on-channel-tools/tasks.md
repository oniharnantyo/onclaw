## 1. Backend: catalog flag and API

- [x] 1.1 Add `AlwaysOn bool` to `ToolCatalogEntry` in `internal/agents/tool_catalog.go`; set it on the `channel.post`, `channel.history`, and `session.close` entries (group/icon keys unchanged)
- [x] 1.2 Add `toggleable` (computed `!AlwaysOn`) and keep `enabled` in `ToolSettingsResponse` (`internal/server/handlers/tools.go`)
- [x] 1.3 Force `enabled: true` for always-on keys in `ToolSettingsService.EnabledTools` and `ViewForWorkspace` regardless of stored rows (`internal/agents/toolsettings.go`) — design D2/D3
- [x] 1.4 Guard `Upsert`/PATCH: `enabled` supplied (non-nil) on an always-on key returns 422 `domain.ErrInvalid` with a user-readable message; config-only patches unchanged
- [x] 1.5 Catalog pin test: every `AlwaysOn` entry is a member of `ChannelToolNames ∪ SessionToolNames` (design risk guard); update `session_tools_test.go` catalog expectations
- [x] 1.6 Service tests: stale `enabled=false` row for a channel key → `EnabledTools` still enabled, view still reports enabled; PATCH `enabled=false` on `channel.post` → 422, row unchanged; config-only PATCH still persists

## 2. Backend: gate behavior

- [x] 2.1 Runner-level test: channel run in a workspace holding `enabled=false` rows for all three keys still resolves `channel.post`, `channel.history`, and (facilitator + open session) `session.close`
- [x] 2.2 Runner-level test: non-channel runs still strip the channel toolset even with stale allowlist keys (existing guarantee, pin it)

## 3. Frontend: icons and mirror

- [x] 3.1 Add `message`, `history`, `check-circle` glyphs to `web/src/components/ui/Icon.tsx` in the existing 24×24 / stroke-1.8 style (design D7)
- [x] 3.2 Add the three keys to `builtinToolNames` and `builtinToolIcons` in `web/src/lib/toolCatalog.ts`; extend its tests
- [x] 3.3 Add `toggleable` to `ApiToolSettings` in `web/src/lib/api.ts`

## 4. Frontend: Tools pane and agent picker

- [x] 4.1 ToolsPane: split the payload by `toggleable`; render the always-on rows under a "Channel Tool" section header above the flat list, with an "Always on · channel runs" / "Always on · facilitator only" badge instead of a toggle and no gear (design D5/D6; ASCII gallery in the change notes is the visual contract)
- [x] 4.2 AgentConfigModal: filter the Built-in Tools picker to `t.toggleable`; keep stale allowlist keys in state on save (they render no chip and save back harmlessly)
- [x] 4.3 ToolsPane tests: Channel Tool section renders three badge rows (no toggle/gear), stale-disabled payload still shows badges, flat list shows the remaining 15 with toggles; AgentConfigModal tests: no chip for the three, stored `channel.post` key stays invisible and survives save

## 5. Smoke and verification

- [x] 5.1 smoke.sh: assert the tools list marks the three `toggleable:false` / others true; PATCH `enabled:false` on `channel.post` → 422; keep existing `ls`/`web.search` toggle coverage passing
- [x] 5.2 `go build ./... && go vet ./... && go test ./...` green; web touched suites green (`ToolsPane`, `AgentConfigModal`, `toolCatalog`, `api`)
- [ ] 5.3 Manual pass: `/settings/tools` shows the Channel Tool section per the approved gallery; agent config Step 3 shows 15 chips; a real channel run exposes all three tools to the model after a deliberate (API) attempt to disable them
