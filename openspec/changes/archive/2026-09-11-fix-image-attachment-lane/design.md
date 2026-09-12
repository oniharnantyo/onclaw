## Context

Three defects interlock in the image/PDF attachment lanes. (1) Wire: `buildAttachmentUserMessage` (internal/agents/attachments_message.go) sets both `URL: <capability URL>` and `Base64Data` on `UserInputImage`/`UserInputFile` blocks, with a comment claiming "backends prefer Base64Data" — eino-ext `agenticopenai` v0.2.2 `resolveURL` (responses_convertor.go) actually returns the URL whenever non-empty, so providers receive a server-relative `/api/v1/files/<token>` URL they cannot fetch (vision models: image parse error; text-only models: content-type rejection). (2) Capability blindness: no component knows whether the turn's model accepts image/PDF input, so any inline attachment on a text-only model fails the whole run. (3) UI silence: neither the config modal nor the composer communicates capability. The capability source already exists: the models.dev catalog cache (internal/services/modelcatalog.go, 24h TTL) carries per-model `modalities.input` and `attachment` fields — e.g. `zai-coding-plan/glm-5.3-flash` lists image+video+pdf input while `glm-5.3` on the same gateway is text-only, which is why resolution must be (provider, model)-scoped.

## Goals / Non-Goals

- Goals: image/PDF attachments work on vision-capable models; degrade gracefully (run succeeds, pointer note) on text-only models; capability visible in the model dropdown and the composer at the moment it matters.
- Non-Goals: no new upload lanes or MIME kinds; no server-side image transcoding; no per-message capability pinning (degradation is per build); no header badges or config-modal-model-list redesign beyond dropdown icons; no change to upload validation, lane classification, tenancy, or capability-URL serving.

## Decisions

### D1: Strip the URL from the model-bound block; keep it in block metadata

The model-bound image/PDF blocks carry only `Base64Data` + `MIMEType` (+ name for files); the connector then builds `data:image/...;base64,` itself. The capability URL moves into the attachment block metadata (`setAttachmentBlockMeta`), which `attachmentMetasOf` already prefers for hydrated pills — it falls back to `meta.URL` when the wire field is empty. Consequences to handle deliberately: the persist-time demotion (which clears `Base64Data`) must leave metadata intact so hydrated pills still resolve the download link, and the URL-only-reference predicate in `attachments_middleware.go` (`Base64Data == "" && URL != ""`) must become metadata-aware or it will misclassify demoted blocks.

### D2: Tri-state input-modality resolution, provider-scoped, from the existing catalog

Add `modalities.input` + `attachment` parsing to `CatalogModel`; expose `SupportsInput(ctx, providerType, model, kind) → supported | unsupported | unknown` on the catalog service. Lookup keys on the mapped community-catalog provider id — `MapProviderType` covers openai/anthropic/google/openrouter; compatible gateways resolve through the new hint (D3). Unknown is an explicit outcome, not an error.

### D3: `catalog_provider` hint on compatible provider configs

Optional field on `openai-compatible`/`anthropic-compatible` provider configs (column + config JSON via migration), editable in the provider form as a select of known community-catalog provider ids. A small built-in host map (api.z.ai, api.zhipuai.cn, openrouter.ai, …) pre-fills the selection; the user's explicit choice always wins. Absent hint = unmapped = unknown (fail-open downstream).

### D4: Degrade at message-build time, fail-open unknown

The decision point is `buildAttachmentUserMessage` — the runtime already knows the turn's provider/model there (it builds the ChatModel from the same config). For an affirmatively-unsupported input kind, the image/PDF block is replaced by a marked pointer note (name, MIME, size, cannot-view notice) in the same style as the drop-lane note; the transcript pill is untouched (it hangs off message attachment metadata, not model blocks). Per-build degradation gives the regeneration property for free: re-run the same turn after switching the agent to a vision model and the rebuild sends the block. Unknown capability sends the block unchanged (fail-open) — a wrongly-missing catalog entry must not degrade a capable model.

### D5: Capability icons ride data the combobox already has

`ModelCombobox` already resolves catalog metadata per (provider, model) for context-window autofill; the four icons (Eye/FileText/Brain/Wrench — reasoning uses Brain per user decision) render from that same resolution in dropdown rows only. Show-if-capable only; no struck-through/dimmed states; tooltips carry the human label. The composer's chip warning reads an input-modality capability field added to the agent payload (computed by the same catalog service at agent read time, alongside `effective_context_window`).

## Risks / Trade-offs

- models.dev data quality gates the degrade path: a wrongly "unsupported" entry silently downgrades a capable model. Mitigations: fail-open-unknown, the explicit hint override, and the pointer note making degradation visible in the transcript.
- B64-inlining increases request size for large images (up to ~6.7 MB base64 for the 5 MB cap) — unchanged from the original design intent (inline lanes were always byte-carrying); the drop lane remains the escape hatch for large files.
- The `catalog_provider` list tracks a moving external catalog; a stale id simply resolves unknown.

## Open Questions

- None — all decisions resolved during exploration (2026-09-11, user-approved gallery for the icon surfaces).

## Live observations (2026-09-11 manual pass, dev)

- Vision model (Research / glm-5.3-flash): image turn 400s with 图片输入格式/解析错误; PDF turn fails earlier in conversion with `unsupported content block type "user_input_file" in user message` — both from the URL-over-base64 connector preference this change fixes. Task 6.4's PDF smoke must cover the file-block conversion path, not just the image path.
- Hydrated transcript CORRECTION: all three pill kinds hydrate correctly — text/PDF render text pills, the IMAGE kind renders an inline thumbnail `<img>` with no text (an innerText-based check will wrongly report it missing; probe the DOM img instead). D1's metadata resolution confirmed for every kind.
