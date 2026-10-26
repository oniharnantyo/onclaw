# Design

## Context

The tool side of this change lands on established rails: the catalog + three-way gate (`internal/agents/builtin_tools.go`, `gate.go`) admits a workspace-scoped direct tool with one catalog entry and one `buildDirectTool` case; tool-generated files already surface in chat through the browser-screenshot bound read (`chat-tool-cards` "Browser screenshot access"); the Tools pane already special-cases configured tools with gears (`ToolsPane.tsx`). What is new: a first dependency on `github.com/alnah/picoloom/v2`, a first multi-row user-managed config table, and a first conditional (tool-offered-dependent) section in the composed turn context. See proposal.md for motivation and the spec deltas for behavior.

## Goals / Non-Goals

Goals: the agent produces a styled PDF from its own markdown with a one-argument styling reference; the user owns presentation through presets; the feature extends to more formats later without reshaping the tool.

Non-Goals: per-call styling overrides; custom CSS or signature blocks in the v1 preset schema; non-PDF formats (docx/xlsx/pptx — the tabs and format column exist, engines do not); live preset preview via picoloom's HTML output (stretch, later); batch/pool conversions.

## Decisions

**D1 — Picoloom v2 as an embedded library.** `go get github.com/alnah/picoloom/v2` and call `NewConverter`/`Convert`/`Close` directly; no CLI subprocess. The CLI's config layering (flags > YAML file > `PICOLOOM_*` env > defaults) is bypassed entirely — the preset editor is the config source. Alternatives: exec'ing the picoloom binary (per-platform packaging burden, config layering mismatch, output parsing); hand-rolling Chrome print-to-PDF (re-implements themes, TOC, footers, cover). Picoloom's use of go-rod means the Chromium binary is the one the browser tool already manages (`~/.cache/rod`, `CHROME_PATH` honored) — error messages for a missing Chrome follow the browser tool's wording.

**D2 — Trust/schema split.** Tool input is exactly `{markdown, filename, preset?}`. Every presentation field lives in presets. Rationale: exposing picoloom's ~30 config fields as tool-input schema would be paid in tokens on every turn of every agent, and CSS/HTML must never come from the model (picoloom strips raw HTML by design; preset CSS is user-authored — that closes the renderer-injection path). If a future need shows up, a single optional cover-title field is the cheapest addition; nothing else is planned.

**D3 — Converter lifecycle.** A manager-held converter created lazily on first invocation, serialized behind a mutex, closed on shutdown. `buildDirectTool` runs on every turn resolve, so constructing per turn would spawn Chrome constantly; per-call construction would pay seconds of Chrome startup per document. A pool is unnecessary for a single-user desktop app — one document at a time, serialized. The adapter package (e.g. `internal/agents/tools/docgen` or beside it) keeps the picoloom types out of the agents package surface, mirroring how browser types stay in `internal/agents/tools`.

**D4 — Preset storage.** A new `document_presets` table (store-assigned id, name unique per format, format defaulting to `pdf`, opaque config JSON, timestamps) behind its own store interface with sqlite and fake implementations, plus a forward-only migration. Not the `tool_settings` table: that is one JSON blob per tool, while presets are multi-row user CRUD. Uniqueness is (name, format).

**D5 — Preset config schema.** The config JSON is a buddies-defined v1 schema (theme, page setup, cover fields, footer, watermark, TOC — no CSS, no signature) mapped onto picoloom's option structs in the adapter. Pinning our own schema keeps the editor form honest, makes future format engines a schema-versioning concern, and prevents a picoloom field addition from silently leaking into presets.

**D6 — Preset awareness rides the operating contract composition.** `internal/agents/contract.go` composes a static runtime contract every turn; the composition gains a conditional appendix listing preset names (default marked) when the `create_document` tool is offered and presets exist. Alternatives: the turn environment block (presets are not environment); a `list` action on the tool (extra per-turn schema weight for information the context can carry for free). The appendix text is composed per turn; the contract's precedence statement is unaffected.

**D7 — Generated-document bound read.** A service read addressed by the plain file name the tool reported, resolved solely inside the calling agent's workspace, refusing directory components before touching the filesystem; the absolute path never crosses to the webview. Mirrors the browser-screenshot bound read. The card body fetches bytes and renders through the existing PDF panel renderer.

**D8 — Output at the workspace root.** The tool writes a plain file name at the workspace root, overwriting on regenerate. Nested output paths would complicate the bound read for no demonstrated need — the jailed `write_file` remains available for staging.

**D9 — Conversion bounds.** The conversion runs under a context timeout (60s — Chrome startup plus rendering for a long document), with failures returned as readable tool errors. Nothing about the tool needs to survive a Chrome crash: the next call relaunches lazily, the same way the browser tool relaunches a profile.

## Risks / Trade-offs

- [go-rod version skew between buddies and picoloom/v2] → resolve at integration: `go mod tidy` and run the browser tool tests alongside; pin the resolved version.
- [picoloom is a small, young library] → the adapter package isolates its types; BSD-3 permits vendoring if upstream stalls.
- [A second headless Chrome during conversion] → lazy, mutex-serialized, transient; acceptable on desktop. Idle converter is closed with the manager rather than reaped, keeping lifecycle simple.
- [Theme rendering varies with platform fonts] → documented limitation; themes are CSS and fall back to system fonts.
- [Preset JSON schema drift] → schema version field in the config JSON from day one; unknown-version presets read but are flagged in the editor.

## Migration Plan

One forward-only migration creating `document_presets` (empty — no presets are seeded). With no presets, the tool uses picoloom's built-in defaults, so the feature degrades gracefully both on deploy and, if ever needed, on rollback of the code (the table simply stays unused; forward-only migrations are not reversed).

## Open Questions

- Exact card presentation (inline preview vs. open-in-panel affordance) can settle during apply; the spec only requires the card offer the PDF through the bound read.
- The HTMLOnly live-preview stretch (rendering preset previews in the dialog) is intentionally out of scope and unspecified.
