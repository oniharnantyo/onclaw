# Tasks

## 1. Schema and store

- [x] 1.1 Migration `000011_provider_model_efforts_video.sql`: `ALTER TABLE provider_models ADD COLUMN efforts TEXT NOT NULL DEFAULT ''` and `ADD COLUMN video_input INTEGER NOT NULL DEFAULT 0`; update the schema-version test pin to 11. Verify: `go test ./internal/store/sqlite/...` passes including the migration-count test.
- [x] 1.2 `store.ProviderModel` gains `Efforts []string` (JSON-encoded in the column, `''` = nil) and `VideoInput bool`; sqlite providermodels read/write maps them; fake store mirrors. Verify: `go test ./internal/store/...` with new round-trip cases (ordered list, empty list, video flag).
- [x] 1.3 Effort normalization helper on the write path (trim, drop empties, dedupe preserving first occurrence, keep order) shared by create/update. Verify: unit test covering `" low"`, duplicates, and empty strings.

## 2. Service and resolution

- [x] 2.1 `ProviderModelInput`/`ProviderModelView` gain `Efforts []string` and `VideoInput`; `CreateModel`/`UpdateModel` persist them. Verify: `go test ./internal/services/...` create/update round-trip cases.
- [x] 2.2 Delete `RefreshProviderModels` (service), the `refreshProviderModels` binding, and `agents.ListModels` (now uncalled); sweep remaining references. Verify: `go vet ./...` and `go build ./...` clean; `grep -rn "ListModels\|RefreshProviderModels" internal/` returns only unrelated matches.
- [x] 2.3 Resolution rule (design D3): `CapabilityResolver.Resolve` tier 1 and `SettingsService.ProviderModels` both return stored efforts when non-empty, else `["high"]`; no-row path (catalog → heuristic) unchanged. Verify: resolver + services tests for the three cases (declared, empty→high, no-row→catalog/heuristic).

## 3. Video lane (backend)

- [x] 3.1 `attachments.laneOf` accepts `video/*` with a 20 MB cap (`MaxVideoBytes`); rejection message updated. Verify: `go test ./internal/attachments/...` accept/reject cases.
- [x] 3.2 `turninput.go`: `video/*` case gated on `VideoInput` + `carriesVideoParts` (gemini-shaped only), emitting `schema.ChatMessagePartTypeVideoURL`/`MessageInputVideo`; `laneName` and the capability warning path cover video. Verify: `go test ./internal/agents/...` — video part on capable carrier, pointer note otherwise, image/PDF regressions green.
- [x] 3.3 Catalog spec structs gain `videoInput` (false for every entry today); `ModelCapabilities` gains `VideoInput`. Verify: `go test ./internal/agents/...` catalog tests pass unchanged.

## 4. Frontend kit

- [x] 4.1 Add `image` and `video` icons to the Icon kit (hand-drawn 24×24 strokes, no lucide dep). Verify: rendered in both themes in the pane (visual check via `task dev`).
- [x] 4.2 New Checkbox primitive (native input, styled box, `check` glyph, label slot) in the onclaw token contract, light+dark parity. Verify: `vp check src` clean; component test for toggle + keyboard activation.

## 5. Providers pane form and badges

- [x] 5.1 Remove the Refresh button, `refreshingModels` state, and the empty-state "or Refresh" clause; rename `refreshError` → `modelsError` (still fed by toggle/delete). Verify: `ProvidersPane` tests updated; no `model-refresh-btn` testid remains.
- [x] 5.2 Replace the Vision/PDF toggles with Image/PDF/Video checkboxes (icons per design D5); rows render a Video chip beside Vision/PDF. Verify: `ProvidersPane` tests — checkbox toggling writes the flags, badges render per row.
- [x] 5.3 Effort rows control: dynamic text inputs with remove buttons, "Add effort" appends, "Leave empty to default to high." hint; add/edit hydrate from the view, save maps to `Efforts` (design D7). Verify: `ProvidersPane` tests — declared order preserved to the input, empty list saves undeclared, composer picks up `["high"]` for undeclared via `ModelCapability`.

## 6. Bindings and gates

- [x] 6.1 Regenerate Wails bindings and run the full gates: `gofmt -w .`, `go vet ./...`, `go test ./...`, `task build` (bindings regen), `vp check src`. Verify: all green; served app shows the new form end to end (add a model with efforts + video, see picker and badges).
