# Tasks

## 1. Domain & schema

- [x] 1.1 Reference-document domain entity: name, description, type, page count, scope (`attached` | `workspace`), index status (`ready` | `no_text_layer` | `processing`), timestamps; validation rules (name format, description length, allowed types); error sentinels. Verify: `go test ./internal/domain/`
- [x] 1.2 Visibility predicate in domain: `visible(run{agent, channel, isChannelSession}, doc)` implementing promoted ∨ agent-attached ∨ channel-attached; unit-test every branch including scheduler-runs-resolve-by-agent-only. Verify: `go test ./internal/domain/ -run Visibility`
- [x] 1.3 New permission-catalog entry for reference-document promotion/demotion (admin-only); ordinary upload/attach rides existing member permissions. Verify: `go test ./internal/domain/ -run Permission`
- [x] 1.4 Migration pair (next head): `reference_documents` (workspace-scoped registry with scope + index status), `reference_document_agents`, `reference_document_channels` joins, `document_sections` (doc, heading, locator, locator_kind, ordinal, body, tsvector + GIN) with down migrations. Verify: `go run . migrate up && go run . migrate status` against a fresh database; `go test -tags=integration ./internal/store/postgres/`

## 2. Stores

- [x] 2.1 Registry store port + in-memory fake: create, get, list (workspace, by-agent, by-channel lenses), update (name/description/scope/attachments), replace, delete. Verify: `go test ./internal/store/ -run ReferenceDocument`
- [x] 2.2 Sections store port + fake: replace-all-for-document, delete-for-document, search (query, visibility-filtered doc ids, LIMIT) returning (document, heading, locator, snippet). Verify: `go test ./internal/store/ -run Sections`
- [x] 2.3 Postgres adapters for both stores with tenant-scoped queries and FTS search (`websearch_to_tsquery` vs `plainto_tsquery` decided behind the single query path, with tests). Verify: `go test -tags=integration ./internal/store/postgres/`

## 3. Indexing pipeline

- [x] 3.1 PDF converter emits page-break anchors in its markdown output so headings map to page locators. Verify: `go test` on the converter package with a multi-page fixture
- [x] 3.2 Format-agnostic sectioner over converted markdown: ATX headings, pptx slide markers, xlsx sheet markers, txt/csv single-section fallback; produces (heading, locator, locator_kind, ordinal, body). Verify: `go test` table-driven across all eight types
- [x] 3.3 Upload service: validate (magic-byte sniff, per-type caps, reject lane) → store blob as `reference` kind via storage port → convert/pass-through → section → index rows → discard text; scanned/no-text-layer PDFs land `no_text_layer` status surfaced in the upload response. Verify: `go test ./internal/agents/` (or owning package) with fake storage
- [x] 3.4 Replace (re-upload) rebuilds sections from the new blob in one transaction; delete cascades blob + sections + join rows. Verify: `go test` for both flows including failure-mid-rebuild recovery via rebuild-from-blob
- [x] 3.5 Capability-URL serving for the `reference` kind reuses the attachment proxy path, byte-identical downloads. Verify: handler test asserting served bytes equal uploaded bytes

## 4. Tools

- [x] 4.1 `document.search` registered against the document family seam (catalog entry, permission key, tool card rule, hook target); implementation queries the sections store with run-visibility-filtered doc ids. Verify: `go test ./internal/agents/` for registration + result shape; catalog snapshot test
- [x] 4.2 `document.read` gains optional `pages` (PDF-only, converter page slices under the existing output cap) and `section` (title match on headings/slide/sheet markers) params; unknown section → structured not-found; `pages` on non-PDF → structured note naming the `section` alternative. Verify: `go test` per scenario in the workspace-document-tools delta
- [x] 4.3 Run-scoped `references/` mount: read-only, composed from run-visible documents only; `ls`/`glob` resolve, write/delete tools reject the subtree; not-visible documents resolve not-found through the mount. Verify: `go test ./internal/agents/` mount composition + jail tests
- [x] 4.4 Document tools gated by the existing workspace tool gate + agent allowlists (no always-on); gate-off agents get no search, no manifest. Verify: `go test ./internal/agents/` gate matrix

## 5. Compose

- [x] 5.1 Manifest injection step: one entry per visible document (name, description, count, top-level TOC) in the compose pipeline at the skills-metadata slot ordering. Verify: `go test ./internal/agents/` compose snapshot with fixture documents
- [x] 5.2 Manifest budget cap with TOC collapse (entries never dropped); usage line for citations ("cite page, slide, or sheet") in the same block. Verify: `go test` budget overflow case

## 6. API & permissions

- [x] 6.1 Upload endpoint (multipart, auth, 201 shape `{id, name, mime, size, url, indexStatus}`) + reject/oversize error envelopes mirroring attachments. Verify: handler tests incl. sniffed-type rejection
- [x] 6.2 List (workspace + `?agent=` + `?channel=` lenses), patch (name/description), delete endpoints with tenancy: cross-workspace ids resolve not-found indistinguishably. Verify: handler tests
- [x] 6.3 Attach/detach endpoints (agents/channels the member can configure) + promote/demote endpoints gated by the new permission entry; scheduler runs excluded from channel scope by the domain predicate (already covered by 1.2, exercised end-to-end here). Verify: handler tests incl. non-admin promote → 403
- [x] 6.4 Router wiring through the composition root with explicit injected dependencies (no nil guards, no fat config). Verify: `go build ./... && go vet ./...`

## 7. Promptdocs

- [x] 7.1 Base-prompt composition lines: reference documents live in `references/`; search with `document.search`; read with `document.read pages/section`; cite locators. Verify: compose snapshot test includes the block; promptdocs fixture updated
- [x] 7.2 Subagent delegation hint: heavy multi-document research routes via the subagent tool with specific questions and citation requirements. Verify: promptdocs fixture + snapshot

## 8. Web — settings Documents pane

- [x] 8.1 Documents API client in `web/src/lib` (upload multipart, list lenses, patch, attach, promote/demote, delete) with tests. Verify: `pnpm test` on the lib
- [x] 8.2 Settings → Documents pane: upload (drag/drop + picker), list rows with type icon, index-status badge, scope badges (ALL AGENTS / n agents · #channels), edit name/description, attach editor, admin promote/demote toggle, replace, delete with confirm; empty and error states per design contract. Verify: component tests; visual pass against `web/Web-Prototype` tokens
- [x] 8.3 Right-panel preview hookup: pane and all document lists open capability-URL preview in the right panel. Verify: component test for the open action

## 9. Web — agent & channel surfaces

- [x] 9.1 Agent config modal read-only "Reference documents" section: promoted + agent-attached with scope badges, preview links, empty state; no edit controls. Verify: component test
- [x] 9.2 Channel settings read-only documents section: channel-attached + promoted with badges, preview links, empty state. Verify: component test

## 10. Web — chat surfaces

- [x] 10.1 `document.search` tool card (query + latency) reusing the document.read card pattern. Verify: component test
- [x] 10.2 Citation chips: markdown capability-URL links render as `📄 name · locator` chips opening the right panel (existing local-file-link behavior, styled). Verify: component test
- [x] 10.3 Composer documents popover: conversation-visible list (direct-chat vs channel predicate lens), preview + insert-as-mention actions, count badge in the composer toolbar. Verify: component tests for both lenses
- [x] 10.4 Mention pointer pill in composer text; turn carries pointer note (document identity only) reusing the drop-lane pointer machinery. Verify: component test + runtime payload test
- [x] 10.5 Right panel Documents source beside Files/Browser listing conversation-visible documents with in-panel preview. Verify: component test

## 11. Integration & verification

- [x] 11.1 Visibility enforced at both layers end-to-end: compose manifest and `document.search` results agree for every scope combination (promoted, agent-attached, channel-attached, cross-workspace). Verify: `go test ./internal/agents/` integration-style fixtures
- [x] 11.2 Subagent inheritance test: spawned child session resolves `document.search` in its registry and receives the manifest for the parent's visibility scope. Verify: `go test ./internal/agents/ -run Subagent` — the design.md D9 regression guard
- [x] 11.3 Smoke sections in `scripts/smoke.sh`: upload PDF + md → index status → `document.search` mixed-type hits → `document.read` page-scoped + section-scoped → manifest presence in composition → visibility filtering (channel-tied doc absent in direct chat). Verify: `./scripts/smoke.sh` full pass green
- [x] 11.4 Full verification sweep: `go build ./...`, `go vet ./...`, `go test ./...`, `go test -tags=integration ./...`, `pnpm test`; fix regressions. Verify: all suites green
- [ ] 11.5 Live pass (user-gated): upload a real service manual in the browser, ask an integration question, confirm search/read cards, page citation chip opens the right panel at the document, channel-tied visibility behaves in a room. Verify: manual checklist recorded in the change
