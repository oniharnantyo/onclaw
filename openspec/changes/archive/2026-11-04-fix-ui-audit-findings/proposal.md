# Proposal

## Why

The 2026-11-02 all-UI Impeccable audit scored the frontend 14/20 and found four P1 defects: the composer's warning text renders at 1.92:1 in the light theme because the spec-mandated `warntext` lifted tier was never defined as a token; streaming does O(transcript) work per token (full repository re-map plus a synchronous full-text markdown re-parse); the entry bundle grew to 6.05 MB with beautiful-mermaid statically bundled and the markdown code-split defeated by three static import chains; and the streaming transcript exposes no live region, so screen-reader users receive none of the streamed text. Beyond the P1s, the audit found the status-color grammar eroding at the vendored seam (emerald/red/amber/blue palette classes instead of contract status tokens, several failing light-theme contrast) and a set of ARIA/keyboard conformance gaps against requirements that are already synced.

## What Changes

- **Contract status-color grammar**: define the missing light-usable lifted text tier for warn (and verify success/danger/accent tiers against 4.5:1 in both themes); re-base the six first-party palette-drift sites (ChatToolGroup, RunSurfaces) onto success/danger tokens; sweep the vendored assistant-ui element layer's status-hue palette classes onto the same tokens.
- **Streaming hot path**: coalesce text deltas in the store's ingest so per-token React/store work is frame-bounded; convert repository messages per-entry so the assistant-ui converter cache hits instead of re-mapping the whole transcript per token; narrow the four whole-`entries` store subscriptions (RunSurfaces, ContextMeter, useLiveAvatar) to stable derivations.
- **Bundle surgery**: load beautiful-mermaid dynamically on the first `mermaid` fence; break the three static import chains that defeat the markdown-text lazy split (or retire the dead `lazy()`); add vendor `manualChunks`.
- **ARIA/keyboard conformance sweep**: live log region on the transcript viewport; valid state attributes on the two `role="button"` + `aria-selected` rows (agents list, providers list); tab/tabpanel wiring with arrow-key movement on the five tab strips; the field focus-within treatment on the composer textarea and combobox search; keyboard-operable resize separators; StatusDot state exposed as text; AvatarPicker trait-select labels and swatch semantics; a real heading for the chat view and heading-level repairs; announced loading skeletons.
- **Polish tail**: 24px targets on the sub-24 icon controls; Tab trap in the Lightbox (kit Modal pattern); run-card blur no longer re-samples over the scrolling transcript; group headers gain `aria-controls`; typing indicator unifies on the kit `od-dot`; the detector's `border-b-2` advisory fixed.
- Amends DESIGN.md: warn text tier documented; palette-class ban stated in the contract.

**Out of scope (recorded, deferred):** the px-locked typography migration (492 arbitrary sizes → named/rem scale — a typeset-scale decision, latent today because the webview exposes no zoom); the sub-xl right-bar overlay redesign (documented design, note only); minimap tick target sizes (hidden below `lg`, dead in desktop); any vendored-code refactor beyond the status-token re-base and focus traps.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `ui-conventions`: extend **Interactive state semantics** (valid state attribute per role, tab/tabpanel wiring with arrow-key movement, status indicators expose text, keyboard-operable resize separators); add **Form control labeling**, **Text field focus treatment**, **View headings**; extend **Status text contrast** into the status color grammar (token-only status hues, palette classes banned, vendored surfaces included).
- `chat-view`: add **Transcript live announcements** (live log region, announced activity/skeleton states); extend **Streaming-safe markdown** with bounded per-delta work so streaming paint latency does not grow with transcript or message length.
- `app-shell`: add **Renderers load on demand** (syntax highlighting, mermaid, and the markdown preview stack stay out of the entry bundle until first use).

## Impact

- Frontend only — no Go services, no bindings regen, no schema changes.
- `frontend/src/styles/tokens.css` + `theme.css` (new `--warn-text` tier, `@theme` mapping), `DESIGN.md` (contract amendment), `frontend/src/styles.css` (detector advisory), kit and chat/agents/settings components listed in design.md, the vendored `components/assistant-ui/elements/*` status classes, `frontend/src/features/chat/store.ts` + `runtime/` (delta coalescing, repository conversion), `frontend/vite.config.ts` (manualChunks).
- Tests: vitest suites for the touched components (several pin current attributes, e.g. agents list and composer), a token contrast check, chunk-size verification in the build gate.
- Verification per repo rules: `npm run test`, `npm run build`, `gofmt`/`go vet`/`go test ./...` untouched-but-run, detector re-run over changed targets, `$impeccable audit` re-score.
- The audit ran on the tree including the uncommitted `add-agent-groups` UI work; fixes land on top of that state.
