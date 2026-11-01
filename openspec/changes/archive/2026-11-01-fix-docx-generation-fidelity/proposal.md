# Proposal

## Why

A DOCX generated from a template preset (`draf-akta-pendirian-pt-oni-harnantyo.docx`, verified on disk) carries two fidelity defects: the output is the uploaded skeleton's full body with the generated draft appended after it (581 paragraphs — the deed appears twice), and the appended pages write inline markdown literally (`**1. Para Penghadap**`, `**ONI HARNANTYO**` — 143 affected paragraphs). Root causes: `renderDOCX` never marks where content belongs in the skeleton (it unconditionally appends), and the block parser's documented stance "inline markup travels as literal text" collides with how models actually write content.

## What Changes

- Template placement contract: a DOCX skeleton MAY carry a `{{content}}` marker paragraph; when present, the generated content replaces the marker (at its position); when absent, content appends after the skeleton's body (today's behavior, now documented as the letterhead-shell contract).
- Inline formatting: the shared block parser resolves `**bold**` and `*italic*` into formatted runs; the DOCX writer emits real bold/italic character formatting. The PPTX placeholder fill strips the markers (plain text). PDF is unaffected (it renders markdown through HTML).
- Model-facing contract text: the create_document description gains one sentence on the marker and on unsupported constructs (tables, code fences, images, horizontal rules degrade to literal text).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `document-generation`: the Document presets requirement gains output-fidelity behavior — template skeletons place content at a `{{content}}` marker when present (append-after otherwise), and generated DOCX text carries bold/italic as character formatting rather than literal markdown markers.

## Impact

- `internal/agents/tools/docgen/` — `content.go` (inline span parsing), `docx.go` (marker placement + span runs), `pptx.go` (marker stripping), plus their tests; `internal/agents/docgen_tool.go` (one description sentence). No stored-data or preset-schema change; existing presets keep working (marker absent = today's behavior). Reuses the splice precedent from the document-editing tooling for marker replacement.
