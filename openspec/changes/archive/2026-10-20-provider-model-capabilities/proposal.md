# Proposal

## Why

The model add/edit form under-declares what a model can do: it offers only Vision and PDF toggles, and it cannot say which reasoning efforts a model supports — efforts are always re-derived from the static catalog or an ID heuristic that returns nothing for unknown ids, so a hand-added model like `laguna-s-2.1` never gets an effort picker in chat. The Refresh button alongside the form is redundant for canonical providers (re-seeded at every startup and creation) and is the last remaining live-network path into the stored-authoritative model list.

## What Changes

- Remove the model list's Refresh button, the `refreshProviderModels` frontend binding, and the `RefreshProviderModels` service (including its live `/models` listing fetch). Live-listed providers (openrouter, openai-compatible, anthropic-compatible) start and stay hand-populated; canonical providers keep their automatic catalog seeding.
- Replace the Vision/PDF toggles in the model form with three checkboxes — Image, PDF, Video — each with its own icon, adding a stored `video_input` flag.
- Add a per-model **reasoning efforts** declaration to the form: a dynamic list of free-form text rows (add/remove, stored in row order). Stored efforts are authoritative for stored rows; an **empty list resolves to `["high"]`** (the one tier every connector family understands). Rows with no declaration stop falling back to the catalog/heuristic; models with no stored row keep today's catalog → heuristic path.
- Accept `video/*` attachments (new size-capped lane) and deliver them as video content parts when the effective model declares video input and the connector can carry them (gemini-shaped carriers, same rule as PDF); every other model degrades them to the reference pointer note.
- Model rows display a Video badge alongside Vision/PDF; two new hand-drawn icons (`image`, `video`) and a Checkbox primitive join the UI kit.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `settings`: "Provider model list" — drop the refresh affordance, change the add/edit fields (Image/PDF/Video checkboxes, reasoning-effort rows), extend row badges, and make stored efforts authoritative with the empty-lists-to-high default.
- `data-store`: "Provider model rows" — the row shape gains an ordered reasoning-effort list and a video-input flag (forward-only migration).
- `chat-attachments`: "Attachment selection and limits" gains a video lane with its size cap; "Model capability degradation" gains video advertisement and the video content-part/degradation path.

## Impact

- Go: migration `000011_provider_model_efforts_video.sql` (+ schema-version test pin), `internal/store` (types, sqlite + fake providermodels), `internal/services/settings_providers.go` (DTOs, create/update, `RefreshProviderModels` deletion, capability read), `internal/agents` (resolver tier-1 efforts, `turninput.go` video lane, `laneOf` in `internal/attachments`), Wails bindings regen.
- Frontend: `ProvidersPane.tsx` (form, badges, refresh removal), UI kit (Checkbox primitive, `image`/`video` icons), composer attachment gating and effort picker (already driven by `ModelCapability.Efforts`).
- Tests: providermodels store tests, settings service tests, resolver tests, `ProvidersPane` tests, attachment/turn-input tests, schema-version count.
