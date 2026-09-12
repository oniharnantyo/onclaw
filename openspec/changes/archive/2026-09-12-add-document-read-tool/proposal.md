## Why

Agents cannot read the documents users attach. Office formats (docx/xlsx/pptx) are rejected at upload, and inline PDFs both crash the OpenAI-family connector (see `fix-document-crashes`) and tax context even where they work. With a single tool the agent can read any document on demand: content reaches the model only when needed, one code path serves every provider, and the same tool family is the seam for the planned document-creation capability (pptx generation, invoices).

## What Changes

- **New built-in tool `document.read`**: input is a jail-validated path (drop-lane attachment mount or agent workspace); conversion runs **in-process in pure Go** through a per-format converter registry — pdf (`ledongthuc/pdf`), xlsx (`excelize/v2`), docx/pptx (stdlib `archive/zip` + `encoding/xml`), html (`x/net/html`), csv/tsv (stdlib); output is markdown text with a size cap and a distinct empty-output result (scanned PDFs). No subprocess, no shell, no Python.
- **Uniform drop-lane routing for documents**: PDF joins office formats in the drop lane — mounted read-only into the run's directory with a pointer note naming `document.read`. The inline-PDF lane retires; the inline lane remains images only (plus the existing fenced inline-text). **BREAKING**: the `inline-pdf` attachment lane no longer exists; PDFs uploaded after this change are drop-lane attachments.
- **Upload reclassification**: docx/xlsx/pptx move from reject to drop lane (rejection message and "export as PDF" guidance removed for modern formats; retained for legacy `.doc`/`.ppt` binaries).
- **Tool-family seam for creation**: the tool registers under a `document.*` family with its own catalog entry, permission key, tool-card formatter, and hook target; `document.create` (later change) becomes a sibling verb with no new plumbing — and its implementation route is free to choose Go-native conversion/generation.
- **Zero runtime provisioning**: there is no converter runtime to install — conversion failures (corrupt or unsupported documents) are per-document structured error results, and the tool is always available.

## Capabilities

### New Capabilities

- `workspace-document-tools`: the `document.read` tool — path validation, in-process pure-Go conversion, output handling, per-document conversion-failure errors — and the `document.*` tool-family seam (catalog, permissions, tool card, hooks) shared with future creation verbs.

### Modified Capabilities

- `workspace-attachments`: lane classification — office formats and PDFs reclassify to the drop lane; the reject lane shrinks to legacy binaries/archives/executables/unknowns; the inline lane becomes images plus small text.
- `agent-runtime`: attachment context lifetime — documents (PDF included) are no longer delivered as native file blocks; the model receives the drop-lane pointer note naming `document.read`. The modality-degradation requirements lose the PDF lane as inline PDFs cease to exist.
- `workspace-tools`: tool catalog gains the `document.read` entry and the workspace tool gate applies to it.

## Impact

- `internal/agents/tool_catalog.go` + a new tool implementation (registration, permissions), `internal/agents/attachments.go` + `internal/domain/attachment.go` (lane classification), `internal/agents/attachments_message.go` (inline-pdf lane retirement), `internal/agents/tools/document_read.go` (in-process converter registry).
- New Go dependencies (all pure Go, no cgo): `github.com/ledongthuc/pdf`, `github.com/xuri/excelize/v2`; `golang.org/x/net/html` promotes indirect→direct. **No Python, no venv, no operator install path.**
- Degrades `fix-document-crashes`'s live gate to dead code for new turns (PDFs no longer inline) — that change lands first and keeps the summarize-window expansion, which remains durable.
- Frontend: upload error copy and attachment chip handling follow existing lane rendering; no new UI components required.
