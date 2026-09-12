## Why

Image attachments always fail at the model call, on every model. Non-vision models reject the whole run (`messages.content.type is invalid, allowed values: ['text']`). After switching to a vision model, the failure changed shape but not outcome (`图片输入格式/解析错误`): the model-bound image block carries both a capability URL (a relative path only the local server can serve) and base64 bytes, and the eino-ext `agenticopenai` connector resolves the URL and ignores the bytes — so the provider receives a URL it cannot fetch. Verified live 2026-09-11 with the Research agent on both model kinds. Beyond the wire bug there is no capability awareness: the image and PDF lanes hard-fail a run on models that cannot see those inputs instead of degrading, and nothing in the UI tells the user which models accept what.

## What Changes

- **Wire fix**: model-bound image and PDF attachment blocks SHALL carry only inline bytes (`Base64Data` + MIME type) so the connector builds a `data:` URL; the capability URL moves into the attachment block metadata, which transcript pill hydration already prefers. The URL-only-reference predicate becomes metadata-aware.
- **Input-modality capability resolution**: the model catalog SHALL resolve whether a (provider, model) accepts image and PDF input — parsed from the models.dev `modalities.input`/`attachment` fields — as a tri-state (yes / no / unknown). Capability is provider-scoped because the same model id exposes different modalities on different gateways.
- **Catalog mapping for compatible gateways**: `openai-compatible` / `anthropic-compatible` provider configs gain an optional `catalog_provider` hint (models.dev provider id) so capability lookups work for custom gateways; a small host-based hint map pre-fills the selection.
- **Graceful degradation at message build**: when the turn's model cannot accept an image or PDF input, the runtime SHALL replace that block with a pointer note (file name, type, size, and the fact that the current model cannot view it) instead of failing the run; the transcript keeps the real attachment pill. Unknown capability SHALL fail open (send the block, today's behavior). Because degradation happens per build, switching the agent to a capable model and regenerating can see the image again.
- **UI — model dropdown capability icons**: the agent config model combobox SHALL render capability icons per option row (eye = image input, file = PDF input, brain = reasoning, wrench = tool calling), shown only when the catalog affirmatively supports the capability; unknown shows nothing. Tooltip = human label.
- **UI — composer attach hint**: when attaching an image/PDF to an agent whose model lacks that input modality, the attachment chip SHALL show a soft warning ("this model can't see images — will attach as reference only"); sending stays enabled.

## Capabilities

### New Capabilities

- (none — all deltas extend existing capabilities)

### Modified Capabilities

- `agent-runtime`: new requirements for model-bound attachment block wire shape and model-modality degradation (additive — the multimodal-turn requirements from the in-flight `add-chat-attachments` change are not redefined here).
- `model-catalog`: new requirement for per-provider input-modality resolution from the catalog.
- `providers`: new requirement for the `catalog_provider` hint on compatible provider configs.
- `web-app/chat`: new requirement for the capability-aware attachment chip hint.
- `web-app/settings`: new requirement for capability icons in the agent model combobox.

## Impact

- `internal/agents/attachments_message.go` (block construction, degrade path), `internal/agents/attachments_middleware.go` (URL-reference predicate), `internal/agents/session_adapter.go` (persist demote keeps metadata URL).
- `internal/services/modelcatalog.go` (parse `modalities`/`attachment`, resolve tri-state), `internal/providers/provider.go` + provider config storage (new optional `catalog_provider` field + migration), `internal/agents/model_factory.go` (pass hint through).
- `web/src/components/ui/ModelCombobox.tsx` (capability icons), `web/src/components/chat/Composer.tsx` (chip warning), agent payload gains an input-modality capability field for the composer.
- No change to upload, lane classification, tenancy, or capability-URL serving — those keep their existing contracts.
- Risk: models.dev data quality gates the degrade path — a wrongly "no" entry degrades a capable model; mitigated by fail-open-unknown and the explicit hint override.
