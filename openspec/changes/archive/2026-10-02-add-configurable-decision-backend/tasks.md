# Tasks

## 1. Provider type: `typesafe`

- [x] 1.1 Register the `typesafe` provider type in the domain catalog as decision-class: key required, `base_url` optional with canonical default `https://api.typesafe.ai/v1/systemone` (http/https override validated like named types), excluded from chat-model resolution and from model-catalog resolution (resolves no models). Verify: `go test ./internal/domain/... ./internal/providers/...` covers the new type's classification, base-URL default/override, and model-list exclusion.
- [x] 1.2 Extend the verify lane: `typesafe` probes the canonical (or overridden) systemone origin with a minimal authenticated one-noul-question request; 200 with an `answers` body → ok:true; 401/403 → ok:false with the provider error; keyless verify → 400. Verify: unit tests for the probe builder and result mapping in the verify handler's test file.
- [x] 1.3 Extend provider delete integrity: refuse 409 when the workspace's memory settings record references the config as `decision_provider_id` (alongside the existing agent-reference check). Verify: store-fake + handler test asserting 409 with the memory-reference message and cleanup behavior after settings clear.
- [x] 1.4 Wire the smoke suite: provider CRUD section covers create/verify/delete of a `typesafe` config including the 409 decision-reference case (mock systemone endpoint). Verify: `./scripts/smoke.sh` passes with the new assertions.

## 2. Memory settings: decision configuration

- [x] 2.1 Add save-time validation on the memory settings handler: `decision_provider_id` + `decision_model` stored together or rejected (400, half-set), provider must exist in the workspace and be decision-class, non-integer/garbage model strings rejected; omitted pair clears both keys (PATCH tri-state precedent). Verify: handler tests for each rejection + the clear-on-omit path.
- [x] 2.2 Extend the settings GET payload to round-trip the pair. Verify: handler test asserting read-back of stored and absent configurations.

## 3. Decision client + intent gate seam

- [x] 3.1 Implement the TypeSafe decision client in `internal/memory`: one `POST /v1/systemone` per classification (state = turn text, model, three fixed noul questions with criteria text), response decode into a verdict, thresholds (default 0.5) with the needs-memory/bucket narrowing rules, single attempt under the caller's context deadline, every failure → self-contained verdict + warn log. Verify: `go test ./internal/memory/...` — table tests over the verdict mapping (below/above threshold, all-buckets-below, malformed body, 401, timeout) asserting the zero/self-contained verdict and no retry.
- [x] 3.2 Branch `IntentGate.Classify`: decision configuration present → client path (key/endpoint/model resolved from the referenced provider per call, tenant-scoped, credential decrypted with the workspace AAD); absent → the existing LLM side-call path byte-identical. Fail-open semantics shared by both paths. Verify: existing `intent_test.go` suite passes unchanged for the LLM path; new tests cover the branch selection with a stored/cleared configuration.
- [x] 3.3 Enforce decision-mode routing limits: the decision path never sets `Associative` or `Entity` on the verdict. Verify: unit test asserting an entity-shaped turn through the decision client yields notes/events routing only.

## 4. Wiring

- [x] 4.1 Composition root: construct the decision client from the provider store + encryption key + settings store and hand it to `IntentGate` in `internal/cli/server.go` (granular params, no nil guards). Verify: `go build ./...` and the wiring test asserting the intent gate resolves with the new dependency.

## 5. Web

- [x] 5.1 `ProvidersPane`: internal Language models | Decision tabs (client-side filter of the loaded list), Decision-tab hint copy, per-tab empty states. Verify: component tests — tab switching, row placement by type, empty states.
- [x] 5.2 `ProviderFormDialog`: type select grouped into "Language models" / "Decision" optgroups; `typesafe` selected → base-URL placeholder = canonical origin, no keyless checkbox, key required. Verify: component tests for the grouped options and the typesafe form shape.
- [x] 5.3 Model-picker filtering: every model-selection surface (agent config modal, workspace default model, memory side-call + embedding selects) excludes decision providers via the shared provider-list helper. Verify: component tests asserting `typesafe` configs never appear in the options.
- [x] 5.4 `MemoryPane` Configuration tab: Decision backend checkbox (default unchecked), checked reveals decision-provider select (typesafe only) + model field prefilled `jev-latest`, save blocked inline when checked without provider, unticking clears the stored pair on save. Verify: component tests for default state, reveal, validation block, and the clear-on-unsave path.

## 6. Verification

- [x] 6.1 Eval fixture A/B: run `internal/memory/eval` with the decision client pointed at a recorded/stub systemone endpoint vs the LLM gate; recall/overall within the pre-registered bounds on the fixture. Verify: `go test -tags=integration ./internal/memory/eval/...` report comparing both backends.
- [x] 6.2 Full verification sweep: `go vet ./...`, `go test ./...`, `go test -tags=integration ./...`, web suite, and `./scripts/smoke.sh` all green; strict OpenSpec validation of all four artifacts. Verify: command outputs in the apply log.
