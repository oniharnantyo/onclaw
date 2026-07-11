# simplify-agent-create — Tasks

Frontend changes in `web/src/pages/AgentDetailPage.tsx` and `web/src/components/Tools.tsx`, plus
one backend edit in `internal/api/service/agent.go` `SetAgentTools` (empty-allowlist symmetric
toggle). No store, schema, API route, CLI, or assembly changes.

## 1. Simplify the create form (`web/src/pages/AgentDetailPage.tsx`)

- [x] 1.1 Remove the create-mode Tools tab: delete the `mode === 'create'` branch that pushes
      `{ id: 'tools', label: 'Tools' }` into `tabsConfig` (~lines 339-341), so create renders
      Overview-only and the tab bar does not render (`tabsConfig.length === 1`).
- [x] 1.2 Change the create default `tools` from `'shell'` to `''` in `DEFAULT_FORM` (~line 34), so
      a UI-created agent carries an empty allowlist (all builtin tools), matching `onclaw agent add`.

## 2. Fix the per-agent Tools tab display (`web/src/components/Tools.tsx`)

- [x] 2.1 Update `isToolEnabled` (agent variant, ~lines 99-106) so an empty/undefined `agentTools`
      returns `true` (all enabled), matching assembly's empty-allowlist = all semantic.
- [x] 2.2 Fix `toggleTool` (agent variant, ~lines 108-158): when the current allowlist is empty
      (all) and the user disables a tool, compute the explicit allowlist as every registry tool name
      (derived from the loaded `categories`) minus the disabled tool. Enabling a tool from the
      empty/all state leaves the allowlist empty. Apply to both the `agentName` (edit, save) and
      no-`agentName` (local-state) paths consistently.
- [x] 2.3 Fix `Service.SetAgentTools` (`internal/api/service/agent.go`): when `agent.Tools == ""`
      (all), disabling a tool stores every other registry tool name (sourced from
      `toolRegistryStore.ListTools`), and enabling is a no-op (stays empty). Without this, a
      disable-from-all was silently dropped on every reload, breaking task 3.6. Covered by a new
      service test `TestService_SetAgentTools_EmptyAllowlist`.

## 3. Verify (no frontend unit-test harness — verify via build, lint, manual)

- [x] 3.1 `cd web && npm run build` (`tsc -b` typecheck + `vite build`) passes.
- [x] 3.2 `cd web && npm run lint` (oxlint) passes.
- [x] 3.3 Backend regression: `make test` (and `go test ./internal/agent/... ./internal/api/...`)
      still passes.
- [x] 3.4 Confirm assembly's empty-allowlist = all-builtin-tools behavior has test coverage in
      `internal/agent` (`TestAssembleAgent_GlobalToolEnable` step 1 exercises it); an explicit
      assertion is added for clarity.
- [x] 3.5 End-to-end (HTTP): create an agent via the API with `tools:""` (the UI create form's
      `DEFAULT_FORM`) → `GET /api/agents/{name}` reports empty tools. Confirmed by the integration
      test `TestWebAgentCreateEmptyToolsAndToggle`. The "run a turn → builtin tools available" half
      is guaranteed by the unit-tested assembly semantic (empty allowlist = all builtin tools,
      `TestAssembleAgent_GlobalToolEnable`); an actual LLM turn needs provider credentials.
- [x] 3.6 End-to-end (HTTP): the agent's Tools tab renders all enabled for an empty allowlist →
      `PUT /api/agents/{name}/tools {tool, enabled:false}` disables one → a second `GET` (reload)
      shows only that tool absent and persists. Confirmed by `TestWebAgentCreateEmptyToolsAndToggle`.
      The browser click-through is the same code path; the LLM-turn half is covered by assembly tests.

## 4. Spec and quality gates

- [x] 4.1 `openspec validate simplify-agent-create --strict` passes.
- [x] 4.2 `make vet` is clean.
