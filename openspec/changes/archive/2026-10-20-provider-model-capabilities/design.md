# Design

## Context

Stored provider-model rows (migration 000010) carry context window, image, PDF, and enabled — but not efforts. Efforts are re-derived at read time from the static catalog or the openrouter-family ID heuristic (`agents.Efforts` / `heuristicEfforts`), which yields nothing for unknown ids, so hand-added models never surface an effort picker in the composer. The model form offers Vision/PDF toggles only; video is rejected at attach time (`attachments.laneOf` accepts image/pdf/text only) and no video lane exists in turn-input assembly. The Refresh button is the sole caller of `RefreshProviderModels`, which is the sole caller of `agents.ListModels`.

## Goals / Non-Goals

**Goals:**
- Stored rows are the whole truth for a model's reasoning-effort set; empty declaration resolves to `["high"]`.
- The model form declares multimodal input as Image / PDF / Video checkboxes and efforts as a dynamic ordered text-row list.
- Video attachments flow end to end: attach lane → stored row flag → video content part on carriers, pointer-note degradation elsewhere.
- The refresh affordance and its entire code path disappear.

**Non-Goals:**
- No changes to connector behavior: effort values still pass through verbatim on OpenAI-shaped endpoints; claude/gemini still map only the fixed budget tiers (`low|medium|high`) — an off-map value silently means "no thinking config" there. Documented, not fixed.
- No catalog/heuristic changes for rows that don't exist: the no-stored-row path (catalog → heuristic) is untouched.
- No audio lane, no per-message attachment-limit changes beyond the video cap.

## Decisions

**D1 — Refresh is deleted, not hidden.** Remove the button, the `refreshProviderModels` binding, the `RefreshProviderModels` service method, and `agents.ListModels` (its only remaining caller). Canonical providers are re-seeded at startup (`SeedCanonicalModels`) and provider creation; live-listed types become hand-add-only. Alternatives: auto-import at creation (network call in the create path, failure semantics muddy an otherwise local operation) and a live-types-only button (keeps a second population semantic alive for no user need). The empty-state copy drops the "or Refresh" clause; the pane reuses its existing error state (renamed from `refreshError` to `modelsError`) for toggle/delete failures.

**D2 — Efforts stored as a JSON array in one column.** Migration `000011_provider_model_efforts_video.sql`: `ALTER TABLE provider_models ADD COLUMN efforts TEXT NOT NULL DEFAULT ''` and `ADD COLUMN video_input INTEGER NOT NULL DEFAULT 0`; no data backfill. `''` = undeclared; otherwise a JSON string array in declared order (order is user-facing — it becomes the composer picker's order). Alternatives: comma-joined text (breaks on values containing commas), a child table (join cost and fake-store plumbing for a ≤4-value list). Write-side normalization: trim each entry, drop empties, dedupe preserving first occurrence, store verbatim otherwise (no case-folding — values ride native request fields).

**D3 — Resolution rule: stored-first, high as the empty default, no-row path unchanged.** Tier-1 (stored row): declared efforts when non-empty, else exactly `["high"]` — no catalog/heuristic fallback for stored rows, making the row authoritative the same way it already is for context window and input flags. `high` is the one tier all three connector families understand (OpenAI native enum, claude budget map, gemini budget map), so the default is universally safe. The rule is applied in both readers: `CapabilityResolver.Resolve` tier 1 and `SettingsService.ProviderModels` (the composer's actual data source). Tier 2/3 (no stored row) keep catalog → heuristic. Alternative considered: seeding catalog efforts into canonical rows (and backfilling existing ones) — rejected: it silently rewrites stored rows at startup and blurs "declared" vs "derived"; accepted consequence: seeded rows offer only `high` until the user declares their set.

**D4 — Effort form control: dynamic text rows.** The form renders one text input per declared value with a remove control, plus "Add effort"; unchecked-by-default empty list with hint "Leave empty to default to high." Free-form values are intentional (vendor-specific tiers like `none` pass through verbatim on OpenAI-shaped endpoints). Alternatives: a fixed four-checkbox row for the normalized tiers (cannot express vendor values — the original pain), OptionChips (chip metaphor implies a closed option set).

**D5 — Multimodal checkboxes on a new Checkbox primitive.** The UI kit has Toggle/OptionChips/Segmented but no checkbox; add a small Checkbox (native input, styled box, `check` icon, label slot) in the onclaw token contract, theme-parity both modes. The form's Inputs row uses three of them — Image (new `image` icon), PDF (existing `file` icon), Video (new `video` icon) — replacing the two Toggles. The Toggle component itself is untouched (used elsewhere).

**D6 — Video is a full lane, mirroring PDF.** `attachments.laneOf` gains `video/*` (cap 20 MB, matching the PDF cap; inline base64 keeps payloads ~1.37×, same tradeoff PDF already accepts). `turninput.go` gains a `video/*` case gated on the model's `VideoInput` flag and a `carriesVideoParts(providerType)` carrier check (gemini-shaped only, same rule as `carriesPDFParts`), emitting `schema.ChatMessagePartTypeVideoURL` with `MessageInputVideo` (verified present in eino). `ModelCapabilities`, `store.ProviderModel`, `ProviderModelInput/View`, and `ModelCapability` all gain `VideoInput`/`Efforts` fields; the catalog spec structs gain `videoInput` (false everywhere today — no catalog model advertises video until the catalogs say so). Composer chips: video rides the existing non-image icon-chip rendering; the capability warning check extends to video.

**D7 — Row badges and DTO shape.** Model rows render a Video chip beside Vision/PDF. `ProviderModelInput` gains `Efforts []string` and `VideoInput bool`; `ModelCapability.Efforts` semantics change to "stored declaration, else `["high"]`". The frontend `startEditModel` hydrates the effort rows from the stored view; saving with zero rows stores `''`.

## Risks / Trade-offs

- [Existing seeded rows collapse to a single-option `high` picker] → Accepted (locked with the user): declaration is now hand-curation; editing a model takes seconds. Mitigated later only if users feel it.
- [Free-form efforts on claude/gemini silently map to no thinking config] → Pre-existing connector behavior, unchanged; design doc records it. The OpenAI-shaped family (where custom tiers actually occur) passes values verbatim.
- [Archive-order dependency: this change's MODIFIED deltas target requirements that only exist once `settings-master-detail` archives] → Validate already flags it as INFO; archive `settings-master-detail` first (it is complete).
- [20 MB video cap still yields ~27 MB base64 payloads] → Same accepted tradeoff as the 20 MB PDF cap; gemini inline-data requests tolerate it; oversize is rejected at attach time, not mid-turn.
- [Schema-version test pins the migration count] → Update the pin to 11 in the same commit as the migration.

## Migration Plan

Forward-only migration 000011 adds nullable-by-default columns (`''` / `0`); no backfill, no rollback path needed (dropping the columns is the reverse, never exercised). Deploy = restart; existing rows read as undeclared → `high`. Rollback of the code leaves two unused columns, harmless.

## Open Questions

(none)
