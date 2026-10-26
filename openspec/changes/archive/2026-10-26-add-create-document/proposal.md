# Proposal

## Why

Agents can write markdown but cannot produce a shareable artifact: when the user needs a styled document — a report, briefing, or invoice — the agent can only dump text into the transcript or write a raw file. A `create_document` tool turns the agent's native markdown into a rendered PDF, with the user — not the model — controlling presentation through saved presets.

## What Changes

- Add a `create_document` built-in tool, v1 rendering markdown to PDF through the embedded `github.com/alnah/picoloom/v2` library. Input is exactly `markdown`, `filename`, and optional `preset`; the PDF is written into the agent's workspace and returned as a workspace-relative path. The agent never sets styling or cover fields.
- Add user-managed **document presets**: named, format-scoped presentation configs (theme, page setup, cover, footer, watermark, TOC) that the agent references by name. A tool call without a preset name uses the configured default preset, falling back to the library's built-in defaults.
- Add a configuration dialog for the tool in the Tools pane (third gear alongside Browser and Web Search) with format tabs — PDF is the only tab in v1 — hosting preset management and the default-preset selector. Future document formats (docx, xlsx, pptx) become new tabs over new engines behind the same tool.
- Present the generated PDF on the tool's chat card, with bytes obtained only through a bound file read (the browser-screenshot precedent).
- Make the agent aware of the available preset names (and the default) through the composed turn context whenever the tool is offered.

## Capabilities

### New Capabilities

- `document-generation`: document presets and their management — what a preset holds, the Tools-pane dialog that manages them (format tabs, default selector), and preset awareness composed into turns.

### Modified Capabilities

- `agent-tools`: adds catalog-entry and tool-behavior requirements for `create_document` (gated like every catalog tool, workspace-scoped, jail applies to the output path).
- `data-store`: adds the `document_presets` table and its persistence semantics.
- `chat-tool-cards`: adds the generated-document card body and the bound read that serves the PDF bytes.

## Impact

- **Go**: `internal/agents` (catalog entry, gate, tool construction, manager-held lazy picoloom converter behind a mutex), a picoloom adapter package, `internal/store` (table, interface, sqlite + fake implementations, migration), `internal/services` (preset CRUD, document bytes read), regenerated Wails bindings.
- **Frontend**: `ToolsPane` gains a third configured-tool gear; new config dialog component with tabs; new `create_document` card body in chat.
- **Dependencies**: `github.com/alnah/picoloom/v2` (v2.1.3) — its go-rod dependency is shared with the browser tool (same `~/.cache/rod` Chromium binary, `CHROME_PATH` honored); go.mod version alignment is a known risk.
- **No new directories** under `~/.buddies`: presets live in the database, generated PDFs land in the agent's existing workspace.
