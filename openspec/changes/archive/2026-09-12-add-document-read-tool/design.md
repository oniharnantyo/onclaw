# Refined: pure-Go document conversion

Supersedes the markitdown/venv design. Decisions D1, D4–D7 survive unchanged; D2/D3 are rewritten.

## Context

The tool/skill/library comparison was resolved to tool-first (explore 2026-09-11): a skill only teaches (the eino skill middleware is progressive disclosure of SKILL.md; execution flows through the agent's own tools, which would route reads through the shell tool's human-approval gate). The original design selected markitdown (Python) because it believed pure-Go libraries covered pdf/docx/xlsx but not pptx. That premise is wrong: a `.pptx` is a ZIP archive of per-slide XML (`ppt/slides/slideN.xml`) the Go stdlib (`archive/zip` + `encoding/xml`) walks directly, exactly like `.docx`. The only format Python genuinely served was PDF text extraction, and `ledongthuc/pdf` (rsc.io/pdf lineage) serves it pure-Go. The venv apparatus is retired; the whole pipeline runs in-process.

Existing machinery this builds on: drop-lane materialization with read-only run-scoped mounts and pointer notes (`attachments_message.go`, D8), the tool registry/catalog with per-tool keys and workspace gate (`workspace-tools` spec), human-readable tool cards, and the jail (`fs_jailed_backend.go`).

## Goals / Non-Goals

- Goals: every agent can read any supported document via one tool; uniform behavior across all six provider types; zero deployment prerequisites beyond the Go binary; the `document.*` family is ready to receive `document.create` without new plumbing.
- Non-Goals: document *creation* (later change — only the family seam lands here); OCR (scanned PDFs surface as empty-output, not images); inline extraction into model context; native PDF file blocks anywhere (the inline-PDF lane retires); shell/venv execution for conversion.

## Decisions

- **D1 — Uniform drop lane for all documents (user-locked, unchanged).** PDF joins office formats in the drop lane on every provider. One behavior everywhere: model-switch stable, no `UserInputFile` blocks ever built (seals the connector crash class structurally), context economy by default. Native-block fidelity (claude/gemini tables/layout) is consciously traded away; if the round-trip UX suffers, a later change can revisit, but the default is uniformity.
  - *Alternative considered:* native inline where the connector supports it — rejected by user choice; keeps provider-dependent behavior and the sanitizer machinery alive forever.
- **D2 — In-process pure-Go converter registry (rewrites the markitdown decision).** `document.read` converts documents in-process. A small registry maps a format (extension + sniffed mime) to a converter function returning markdown; the tool resolves the format, invokes the converter, and shapes the output. No subprocess, no shell, no venv — bounded instead by a conversion deadline on the context and the upload-time size caps already enforced. New formats are new registrations against the same seam (plugins-are-first-class).
  - Locked library set (all pure Go, no cgo): **pdf** → `github.com/ledongthuc/pdf`; **xlsx** → `github.com/xuri/excelize/v2` (sheet-per-markdown-table); **docx** and **pptx** → stdlib `archive/zip` + `encoding/xml` (paragraph/heading/table walk over `word/document.xml`, `ppt/slides/slideN.xml`); **html** → `golang.org/x/net/html` (already in the module graph, promoted indirect→direct); **csv/tsv** → stdlib `encoding/csv`.
  - *Alternatives considered:* markitdown from the shared venv — rejected (user decision: prefer Go; kills the Python deployment requirement); `pdfcpu` for PDF — rejected, its extraction surface is weaker for linear text than ledongthuc's; `fumiama/go-docx` — rejected, the stdlib walk is smaller than the dependency.
- **D3 — Conversion failures are per-document structured results (rewrites the degraded-environment decision).** There is no conversion runtime left to go missing: the environment-degradation error class ceases to exist. A corrupt, password-protected, or otherwise unconvertible document returns a structured error result naming the document and the failure — the run continues, and the tool remains registered and selectable unconditionally. Availability is structural, not a runtime condition.
  - *Alternative considered:* keeping the runtime-missing error shape for parity — rejected: an error message about installing something that cannot be missing is noise to the model.
- **D4 — Path validation reuses the jail (unchanged).** The tool accepts only paths the jailed backend already exposes: the run's drop-lane mount and the agent workspace tree. Path traversal outside the jail is a tool-parameter rejection (same contract as `read_file`), not a new security surface.
- **D5 — Output contract: markdown in, capped, with explicit emptiness (unchanged).** Successful output is markdown text truncated at a cap (aligned with inline-text's 200 KB ceiling) with a truncation notice; a converter returning no extractable text (scanned/image-only PDF) yields a distinct "no extractable text" result so the model can tell the user instead of guessing. The tool result carries the document name for tool-card rendering.
- **D6 — `document.*` family lands now, creation later (seam unchanged; future-verb note revised).** The registration, catalog entry shape, permission key, tool-card verb ("read"), and hook target are built family-first so `document.create` is a new verb on an existing seam. Its implementation route is no longer pre-committed to python-pptx-in-the-venv (the venv is gone); that decision belongs to the creation change.
- **D7 — Lane retirement is additive in storage (unchanged).** Persisted attachments keep their historical `inline-pdf` lane values (transcript chips keep rendering); only new uploads classify under the new rules. The runner treats an incoming `inline-pdf` ref from old state as a drop-style pointer (its bytes are gone post-persist anyway — reference form).

## Risks / Trade-offs

- [Go PDF text extraction flattens tables/layout more than markitdown's pdfminer] → Acceptable for the read-to-model use case; the raw toggle on tool cards lets users inspect output. Swapping or augmenting the pdf converter later is inside the registry — one registration.
- [Converter edge cases (encrypted PDFs, exotic OOXML) surface at read time] → They return per-document structured errors, not crashes; the pointer note and transcript pill keep the file visible either way.
- [Agent skips the read and answers from the filename] → Pointer-note copy is imperative and names the tool; a system skill teaching read-policy (whole-doc vs grep) can follow. Fundamental pull-model trade-off, user-accepted.
- [Two new direct Go dependencies (ledongthuc/pdf, excelize/v2)] → Both pure Go, no cgo; the single-binary deployment story is preserved and actually strengthened (no Python anywhere).

## Migration Plan

No DB migration: lane values are stored strings, historical `inline-pdf` rows keep rendering (D7). Deploy order: this change after `fix-document-crashes` (its live gate becomes dead code but its summarize expansion stays). Rollback is a revert; post-revert, new PDF uploads reclassify inline and the gate again applies. No operator action of any kind — the conversion engine ships inside the binary.

## Open Questions

- Truncation cap value (200 KB ceiling vs a larger document-specific cap) — settle at implementation against real converter outputs; the spec fixes only that a cap and truncation notice exist.
- PDF extraction quality acceptance (ledongthuc/pdf on real-world documents) — verify in the manual pass (task 4.2) with representative documents; a converter swap is a one-registration fix if it disappoints.
