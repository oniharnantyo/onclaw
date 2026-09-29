# Proposal

## Why

A live run (2026-09-28, Personal Assistant, "sokratech PostLogin") needed 20 tool steps to answer from a document `document.search` had already located on step 1 (8 hits, 17 ms, correct locators). The agent then: hunted the file through the shell (two unjailed full-disk `find /` scans at 60 s each), called `document.read` with a macOS firmlink spelling of the mount path (`/System/Volumes/Data/...`) that the tool's lexical jail check rejected in 0 ms, never retried with the documented `references/<name>` form because the error taught nothing, and finally hand-rolled a python docx parser. Turn 1 ("give me PostLogin payload") never searched at all — the manifest's 6-entry TOC hid the `5.1 PostLogin` heading and the description was empty, so nothing in context connected the request to the document.

## What Changes

- Manifest injection projects all top-level (level-1) headings per document — not a 6-entry mixed-level prefix — within the existing character budget, so section names like `5.1 PostLogin`…'s parent become visible before the first tool call.
- `document.search` hits carry a ready-to-use read hint naming the exact next invocation (document path + locator) so the model never improvises a filesystem path.
- `document.read` canonicalizes macOS firmlink path spellings (`/System/Volumes/Data/...` ≡ `/...`) before the jail-root comparison, so the same file is not rejected for its spelling.
- `document.read` path errors teach the accepted forms (`references/<document name>`, workspace-relative, `/workspace/...`) instead of a bare rejection.
- Base prompt gains one guardrail line: reference documents are already mounted at `references/` — use `document.search`/`document.read`, never the shell.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `workspace-reference-documents`: "Manifest injection" changes — top-level table of contents SHALL cover the document's level-1 headings. "Document search tool" changes — each hit SHALL carry a read hint naming the document path and locator. New normative line under "Deterministic section index" behavior is unchanged; path canonicalization and error copy are tool-behavior requirements recorded under a new "Document read path handling" requirement.

## Impact

- `internal/references/service.go` (`TableOfContents` level filter) and `internal/agents/references_manifest.go` (projection) — manifest heading coverage.
- `internal/agents/tools/document_search.go` — hit result gains a read hint field.
- `internal/agents/tools/document_read.go` — canonical path comparison + teaching error copy.
- `internal/promptdocs/AGENTS.md` — one guardrail line.
- Tests for each; no API, schema, or frontend change.
