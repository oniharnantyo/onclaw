# Proposal

## Why

Enabling **PDF input** on a provider model has no effect for most providers: the turn's model-input assembly ANDs the model's stored capability flag with a hardcoded `carriesPDFParts` gate that only passes `gemini` (`internal/agents/turninput.go`), so every other provider — including the whole anthropic family, whose pinned `agenticclaude` connector already maps base64 files to native PDF document blocks — degrades PDFs to a pointer note that falsely claims "the active model cannot read PDF input". The composer then shows no warning (it only warns when the model flag is off), so the user gets zero signal. Worse, the degradation is a dead end: the note exposes only an attachment key with no instruction, the one agent-reachable route it implies (`save_document` into the knowledge corpus) misuses a curated durable corpus as an upload landing zone, and reading a PDF back through `read_file` currently **kills the turn on every connector family** (tool-result file parts: the OpenAI chat conversion has no file case, gemini tool results carry text only, and claude's document-in-tool_result mapping is unverified against the API).

## What Changes

- **Per-family carriage matrix**: PDF file parts ride the user message for `gemini`, `anthropic`, and `anthropic-compatible` providers when the model's stored row advertises PDF input (the checkbox stays the per-model gate; for *-compatible endpoints it is the user's assertion that the endpoint passes document blocks). Video carriage is unchanged (gemini only) but moves onto the same matrix function.
- **Turn-time uploads mirror**: every attachment riding a turn is copied (if absent) into the agent's workspace at `uploads/` — host path `workspaces/<slug>/uploads/`, model-visible `/workspace/uploads/<name>` — so the filesystem tools can reach uploaded content without any new mount, jail root, or trust boundary.
- **Actionable, truthful pointer note**: degradation notes name the real reason (model lacks the capability vs. provider connector cannot carry the kind) and hand off to `/workspace/uploads/<name>` instead of a bare capability key. The office-document lane's note switches from "save into knowledge" to "read it from your uploads".
- **Read fallback for uncarriable kinds**: when the turn's connector family cannot carry a kind in tool results (OpenAI family: files; gemini: files), `read_file` on such a file serves extracted text (PDFs, via a small extraction seam) or a readable explanation instead of a block that errors the turn.
- **`save_document` becomes pure opt-in curation**: unchanged behavior, but no degradation path routes through knowledge anymore; its model-facing note references move to the uploads path.
- **Composer warning reflects carriage**: the frontend warns "PDF will arrive as a reference note" when the session's model flag is on but the provider family cannot carry PDF parts, instead of staying silent.
- **Spec amendments**: chat-attachments' storage requirement is amended to allow the workspace-visible mirror copy (the model may be told the workspace-relative path; the frontend still sees capability URLs only), and its degradation requirement gains the connector-carriage clause for PDF that video already has.

## Capabilities

### New Capabilities

<!-- none — this change modifies existing capabilities only -->

### Modified Capabilities

- `chat-attachments`: storage requirement gains the turn-time workspace uploads mirror and rewords the path-exposure line (model may be told `/workspace/uploads/...`; frontend unchanged); model capability degradation requirement gains the per-family connector-carriage clause for PDF, truth-in-reasoning for pointer notes, and the uploads handoff.
- `agent-tools`: the `read_file` multimodal rendering requirement gains a degradation rule — a kind the connector cannot carry in tool results degrades to extracted text (PDF) or a readable explanation instead of a turn-failing block.

## Impact

- **Go backend**: `internal/agents/turninput.go` (carriage matrix, note contract), `internal/agents/connectors.go` or a new carriage module (per-family matrix), turn assembly / manager (uploads mirror, `turnComposer` gains the agent workspace dir), `internal/agents/compose/compose.go` + `internal/agents/backend/fs_jailed_backend.go` (carriage-aware `MultiModalRead`, text-extraction seam), `internal/agents/knowledge_tool.go` (description wording only).
- **Frontend**: chat composer capability warning uses carriage, not just the model flag; the capabilities binding payload grows a carriage field (Wails bindings regenerate).
- **Dependencies**: PDF text extraction reuses a pure-Go extractor behind a narrow seam (pdfcpu is already in the dependency tree via document-editing; if its extraction proves inadequate, the seam swaps implementations without touching the design).
- **Verification**: unit tests per matrix cell and mirror behavior; live smokes — Z.AI (`anthropic`) direct PDF attach and `read_file` from uploads; an OpenAI-family provider exercising the text-extraction fallback; a checkbox-off turn confirming the note handoff.
- **Not in scope**: wiring or deleting the dead `SupportsResponsesAPI` (the Responses path is a separate route to OpenAI-family PDFs, tracked as follow-up), gemini's inability to carry *images* in tool results (pre-existing, adjacent — recorded in design as a known gap), and video carriage beyond gemini.
