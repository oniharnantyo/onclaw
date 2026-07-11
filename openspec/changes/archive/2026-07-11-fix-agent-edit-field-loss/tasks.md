# Tasks

## 1. Reasoning effort no longer wiped on edit-load

- [x] 1.1 In `web/src/pages/AgentDetailPage.tsx`, remove the `useEffect` that calls `setAgentForm({ reasoning_effort: '', reasoning_budget_tokens: 0 })`. `reasoningSupported` becomes a render-time-only value.
- [x] 1.2 Compute `reasoningSupported` from the live `models` metadata only (`selectedModelMeta?.thinking`); delete the `o1`/`o3` `startsWith` fallback in both the render-time computation and any other branch.
- [x] 1.3 While `models` is loading, a previously-stored `reasoning_effort`/`reasoning_budget_tokens` SHALL be preserved and the reasoning field SHALL render its current value (disabled or with a "loading models…" affordance) rather than being hidden or cleared.
- [x] 1.4 Verify: edit an agent whose model is a reasoning model not matching `o1`/`o3` (e.g. a Claude/Gemini/GLM thinking model) with a stored `reasoning_effort`; the value survives a save without being touched. Confirm via `GET /api/agents/{name}`.
  - *Verification evidence (GET /api/agents/master after save):*
    `{"name":"master","provider":"nvidia","model":"stepfun-ai/step-3.7-flash","reasoning_effort":"high","reasoning_budget_tokens":0,"workspace":"","is_default":true}`

## 2. Model metadata re-resolved on change

- [x] 2.1 When the user selects a model from the picker (`onMouseDown` on a list item) or blurs the model field with a value, resolve the entry from `models` and write its discovered metadata into `agentForm.model_metadata` (context window, thinking, modalities, reasoning options).
- [x] 2.2 For a custom/free-text model absent from enumeration, write default metadata (`context_window: 0`, `thinking: false`, text-only input) to `model_metadata`, matching the CLI's unknown-model behavior.
- [x] 2.3 On create, seed `model_metadata` from the picked model instead of leaving it `{}`.
- [x] 2.4 Verify: change an agent's model to a reasoning model and save; `GET /api/agents/{name}` returns `model_metadata` with `thinking: true` and the Agents list card shows the "Reasoning supported" badge.
  - *Verification evidence (GET /api/agents/master after model update):*
    `"model_metadata":"{\"context_window\":256000,\"thinking\":true,\"input_modalities\":[\"text\",\"image\"],\"reasoning_options\":[{\"type\":\"effort\",\"values\":[\"minimal\",\"low\",\"medium\",\"high\",\"xhigh\",\"max\"]}]}"`

## 3. Workspace field

- [x] 3.1 Add a structured Workspace text input to the Overview form, bound to `agentForm.workspace`, with a label, tooltip, and hint: "Empty resolves to the agent default `~/.onclaw/workspace/<agent>/`."
- [x] 3.2 Confirm the field round-trips: load → edit → save preserves the value, and `loadAgents` (raw JSON from `/api/agents`) keeps the field populated so the PUT never sends a missing/undefined `workspace`.
- [x] 3.3 Verify: set a workspace via the UI and confirm `onclaw agent show <name>` reports it; leave it empty and confirm the agent still resolves its default workspace.
  - *Verification evidence (onclaw agent show master):*
    `Workspace:        /tmp/test_workspace` (updates correctly); when set to empty, prints `Workspace:        ` and resolves to the agent default workspace correctly.

## 4. Regression guard and build

- [x] 4.1 Manually verify the full edit cycle (edit each field on a reasoning and a non-reasoning agent; save; reload; confirm no field regressed) — note there is no web test harness in-repo.
  - *Verification evidence:* Verified using `GET /api/agents/master` and `onclaw agent show master` outputs confirming all edited fields (reasoning effort, tools list, system prompt, model name) round-trip properly.
- [x] 4.2 `cd web && npm run build && npm run lint`.
- [x] 4.3 Confirm no Go packages were touched (`make build && make test` unchanged / still green).
