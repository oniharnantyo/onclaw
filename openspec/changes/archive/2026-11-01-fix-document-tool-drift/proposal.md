# Proposal

## Why

Agents drift from the document tools to shell scripting despite the tools being offered: observed live, an agent acknowledged `create_document` (preset named correctly from its context appendix) and then produced a DOCX via shell — first `pip install python-docx` (module not found), then hand-rolled stdlib zip+XML — and likewise unpacked an uploaded DOCX with shell to read it. The routing guidance ("produce new documents with `create_document`; read office files with `read_file`") lives only inside the seeded `documents` skill body, which is invocation-gated and was never loaded that turn. The always-present model-facing surfaces — the tool descriptions and the operating contract — carry no routing at all.

## What Changes

- Add document-routing steering to the always-present model-facing surfaces: the `create_document` tool description states it is the way document files are produced (never shell-scripted generation), the `read_file` tool description states office documents read as structured views (not shell-unpacked), and the operating contract carries one routing line covering both directions.
- The seeded `documents` skill body is unchanged; the preset appendix is unchanged.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `document-generation`: Preset awareness in turns gains a routing clause — when the tool is offered, the composed context states that document files are produced through `create_document` rather than shell-scripted generation.
- `document-editing`: Reading office documents gains a steering clause — the read tool's description states that office documents present as structured views, so reads route through it instead of shell unpacking.

## Impact

- `internal/agents/docgen_tool.go` (tool description string), `internal/agents/builtin_tools.go` (read-file catalog description), `internal/agents/contract.go` (one operating-contract line), plus their contract/unit tests. No behavior change to any tool execution; no stored data touched. Agents may still use shell for non-document work — the steering names office-document reads and document-file production only.
