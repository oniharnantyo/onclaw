## 1. Wire fix: model-bound blocks carry bytes

- [x] 1.1 `internal/agents/attachments_message.go`: inline-image block construction sets `Base64Data` + `MIMEType` only; capability URL moves into the attachment block metadata (`setAttachmentBlockMeta` gains the URL)
- [x] 1.2 Same change for the inline-PDF block construction
- [x] 1.3 `internal/agents/attachments_middleware.go`: make the URL-only-reference predicate (`isURLRefOnly`) metadata-aware so persist-demoted blocks (bytes stripped, URL in meta) classify correctly
- [x] 1.4 `internal/agents/session_adapter.go` persist demotion: assert/verify metadata (with URL) survives the serializer round-trip; adjust if the demote path touches the new field placement
- [x] 1.5 Unit tests: built image/PDF blocks carry no URL field; `attachmentMetasOf` yields the capability URL from metadata; hydrated pill URL unchanged; demoted block still classified as a URL reference
- [x] 1.6 Manual/live check with a vision model: image attachment turn completes (no `图片输入格式/解析错误`, no content-type 400) — VERIFIED 2026-09-11 live (Research/glm-5.3-flash): canvas PNG ("73") pasted and sent; model answered "73"; also required applying migration 000046 (the implementing session left it unapplied — provider load 42703'd on `catalog_provider`)

## 2. Catalog: input-modality resolution

- [x] 2.1 `internal/services/modelcatalog.go`: parse `modalities.input` and `attachment` from catalog model entries; keep the parsed fields on `CatalogModel`
- [x] 2.2 Add tri-state resolution `SupportsInput(ctx, providerType, catalogHint, model, kind) → supported | unsupported | unknown` (kinds: image, pdf), keyed on the (mapped provider | explicit hint, model id)
- [x] 2.3 Unit tests: capable gateway entry → supported; same model id on a text-only gateway → unsupported; unmapped provider without hint → unknown

## 3. Providers: catalog-mapping hint

- [x] 3.1 Domain + storage: optional `catalog_provider` field on openai-compatible/anthropic-compatible provider configs (config JSON; column/migration only if the schema stores it relationally)
- [x] 3.2 Handlers/validate: accept and persist the hint; reject values not present in the community-catalog provider id list
- [x] 3.3 Built-in host-hint map (api.z.ai, api.zhipuai.cn, openrouter.ai, …) used to pre-fill the form selection; explicit user selection always wins
- [x] 3.4 Web provider form: hint select (optional, "Auto-detect from host" default) on compatible provider configs
- [x] 3.5 `model_factory.go`: pass the hint through to catalog resolution call sites
- [x] 3.6 Unit tests: hint present → resolution uses the mapped gateway; absent + unknown host → unknown

## 4. Runtime degradation

- [x] 4.1 Resolve the turn's input-modality capability in `buildAttachmentUserMessage` (provider type + hint + model are already in scope for ChatModel construction)
- [x] 4.2 Unsupported image/PDF input → replace the block with a pointer note (file name, MIME, human size, cannot-view notice) styled like the drop-lane note; transcript pill untouched
- [x] 4.3 Unknown capability → send the block as-is (fail-open); supported → send bytes (per group 1)
- [x] 4.4 Unit tests covering the four spec scenarios: degrade on text-only, whole-block on vision-capable, regenerate-after-model-switch rebuild, fail-open unknown
- [x] 4.5 Integration test: a turn with an inline image on a text-only model completes and the transcript records both the user pill and the degraded pointer note

## 5. Web: capability surfaces

- [x] 5.1 Agent payload: add an input-modality capability field (image/pdf tri-state) computed by the catalog service at agent read, alongside `effective_context_window`
- [x] 5.2 `ModelCombobox` dropdown rows: capability icons Eye (image), FileText (pdf), Brain (reasoning), Wrench (tool calling) — show-if-capable only, tooltip labels, reuse the combobox's existing catalog resolution; unit tests per jsdom conventions (select mocks must include every option a test exercises)
- [x] 5.3 Composer: chip warning "this model can't see images — will attach as reference only" (and the PDF variant) when the agent's capability is affirmatively unsupported; send stays enabled; no warning when unknown; unit tests both states
- [x] 5.4 `pnpm build` green; vitest touched suites green (judge by touched suites, not full-parallel counts)

## 6. Verification

- [x] 6.1 Live browser pass, vision model (Research on glm-5.3-flash): image attachment turn succeeds; model describes the attached image; transcript pill + download link intact after reload — VERIFIED 2026-09-11; NOTE the image pill hydrates as an inline THUMBNAIL `<img>` (no text), so innerText-based checks miss it — verify via DOM (img complete + naturalWidth > 0). First live enrichment attempt returned no capability flags for hint-less gateways: `catalogMapping` lacked the host-hint fallback the agent payload had — fixed to use `SuggestCatalogProvider` (models API now returns image/pdf/reasoning/tool flags for api.z.ai)
- [x] 6.2 Live browser pass, text-only model (agent pinned to glm-5.3): image attach shows the chip warning; turn completes; transcript shows the pointer note and the pill — VERIFIED 2026-09-11 on QA Engineer (glm-4.7, text-only per catalog): warning chip shown, send enabled, turn completed, model received the pointer note (replied it can't view images), thumbnail pill rendered. Also fixed live: the store's agent mapping dropped `input_modalities` (field never reached the Composer) — added to web/src/store/index.ts
- [x] 6.3 Model dropdown: icons render per catalog entry (glm-5.3-flash ◉▤Ⓑ⚒ vs glm-5.3 Ⓑ⚒ vs unknown gateway quiet); unknown model rows stay quiet — VERIFIED 2026-09-11 (Z.AI provider, live source): glm-5.3-flash Eye+FileText+Brain+Wrench, glm-4.7/glm-5.2/glm-5.3 Brain+Wrench, glm-4.5/glm-4.6/glm-5/glm-5.1 quiet (no catalog entry), Custom-model quiet
- [x] 6.4 PDF lane smoke (per D4 degrade semantics): PDF on an affirmatively-PDF-unsupported model completes end-to-end as a degraded reference-only turn — VERIFIED 2026-09-11 evening on QA Engineer (glm-4.7): chip warning "this model can't read PDFs — will attach as reference only", run completes, model receives the marked pointer note (AttachmentPointerExtraKey) and says it cannot view PDFs, chip hydrates after reload with meta + download link; a pdf:true model (glm-5.3-flash) sends the UserInputFile block fail-open and still fails at the pinned eino-ext chat-completions converter (`unsupported content block type "user_input_file"`) — the residual PDF-comprehension gap is upstream (Anthropic/Gemini paths carry file blocks natively), recorded in add-chat-attachments 11.4
- [x] 6.5 `go build ./...`, `go vet ./...`, `go test ./...` green; smoke suite section for attachments green — VERIFIED 2026-09-11 evening: build/vet clean, all untagged packages green, full integration suite green (fixed `providers_catalog_test.go:126` information_schema probe to filter `table_schema = current_schema()` — the unfiltered probe matched the dev DB's public-schema copy and false-failed on any v46 host), smoke 521/521
