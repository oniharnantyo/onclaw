# Spec Delta

## MODIFIED Requirements

### Requirement: Manifest injection
For every agent run with document tools enabled, composition SHALL inject a reference-documents manifest: one compact entry per reference document visible to the run — name, one-line description, page (or equivalent) count, and the document's top-level (level-1) headings as its table of contents. The table of contents SHALL represent the whole document's chapter structure, not a prefix of its first sections. The manifest SHALL be budget-capped; entries exceeding the budget SHALL collapse their table of contents to the document name and description rather than being dropped. The manifest SHALL NOT be injected for runs whose agents lack document tools.

#### Scenario: Manifest names the library
- **WHEN** an agent with document tools starts a run in a workspace holding two reference documents
- **THEN** the agent's context includes both documents with descriptions and top-level TOCs

#### Scenario: Deep section is discoverable from the manifest
- **WHEN** a document's PostLogin payload section is its chapter 5 while chapters 1–4 precede it
- **THEN** the manifest's table of contents for that document includes the chapter-5 heading, so a request about PostLogin can be matched to the document before any tool call

#### Scenario: Budget collapse keeps discovery
- **WHEN** the workspace holds more documents than the manifest budget allows at full detail
- **THEN** later entries collapse to name and description, and no visible document disappears from the manifest entirely

### Requirement: Document search tool
The runtime SHALL expose a built-in tool `document.search` registered against the document tool family seam (catalog entry, permission key subject to the workspace tool gate, tool card, hook target — no new plumbing). Its input is a text query; its output is a bounded list of matching sections across all reference documents visible to the run, each hit naming the document, its heading, its locator when one exists, and a text snippet. Each hit SHALL carry a read hint naming the exact `document.read` invocation for that hit — the mount-relative document path and its locator — so the consuming model can read the section without constructing filesystem paths. Search SHALL be full-text over the section index without embeddings or model calls.

#### Scenario: Mixed-type hits for one query
- **WHEN** the library holds a PDF, a markdown note, and a CSV mentioning "sandbox rate limit" and the agent invokes `document.search` with that query
- **THEN** the result lists hits from all three documents, each with its own locator form (page, heading, none)

#### Scenario: Hit names its next read
- **WHEN** a search hit resolves to section "5.1 PostLogin" of `FDS_Sokratech_Spesifikasi_Input_Output.docx`
- **THEN** the hit carries a read hint equivalent to reading `references/FDS_Sokratech_Spesifikasi_Input_Output.docx` scoped to that section

## ADDED Requirements

### Requirement: Document read path handling
The `document.read` tool SHALL treat different absolute spellings of the same file as the same file when checking its read roots — on macOS, the `/System/Volumes/Data/...` firmlink spelling and the plain `/...` spelling of one path SHALL both pass the root check. When a path is rejected as outside the allowed roots, the error SHALL name the accepted path forms — the `references/<document name>` mount form, the workspace-relative form, and the `/workspace/...` mount form — so a model can self-correct in one retry.

#### Scenario: Firmlink spelling accepted
- **WHEN** a document blob is reachable at `/Users/me/.onclaw/.../references/manual.pdf` and the tool is invoked with the `/System/Volumes/Data/Users/me/.onclaw/.../references/manual.pdf` spelling of the same file
- **THEN** the read succeeds, returning the same converted content

#### Scenario: Rejection teaches the correct form
- **WHEN** the tool is invoked with a path outside every allowed root
- **THEN** the error names `references/<document name>` and the workspace-relative form as accepted alternatives instead of a bare rejection
