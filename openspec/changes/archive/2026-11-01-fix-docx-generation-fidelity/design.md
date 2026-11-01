# Design

## D1: Evidence

`~/.buddies/workspaces/jono/artifacts/draf-akta-pendirian-pt-oni-harnantyo.docx` (31197 bytes ≈ the 31323-byte template): 581 paragraphs, prefix is the skeleton's own deed text, 143 paragraphs carry literal `**…**` / `- ` markers — including generated data (`**ONI HARNANTYO**`, the user's address), so the markers are in the APPENDED content, not the skeleton. Code: `renderDOCX` (`internal/agents/tools/docgen/docx.go:34`) opens the skeleton and unconditionally `appendDOCXBlocks`; `parseBlocks` (`content.go:43`) maps block-level constructs only, by design ("inline markup travels as literal text").

## D2: Marker placement

`renderDOCX` with a skeleton: scan the opened document's paragraphs (`domain.Document.Paragraphs()`, whose `Text()` reads plain text) for the `{{content}}` marker.

- **Marker present**: render the content blocks into a fragment and replace the marker paragraph at its body position. Precedent: the document-editing tooling already edits Word XML by splice (`internal/agents/tools/documents/docx.go` `spliceDeleteParagraph` / `spliceInsertParagraph`), so byte-level body surgery is established in-repo. Concretely: render the blocks into a scratch document via the same builder the append path uses, take its body fragment, and splice it over the marker paragraph's XML in the template package before `WriteTo`. `{{title}}` fill (existing `MergeTemplate`) runs first, unchanged.
- **Marker absent**: today's append behavior, unchanged — the letterhead-shell contract.
- The marker paragraph's style does not leak into the fragment (the fragment carries its own styles).

Fallback ordering: if the scratch-fragment splice proves fragile against Word's run-split XML, degrade to "delete marker paragraph + insert content at its index" using the splice helpers' indexing. Either way the marker never survives to the output.

## D3: Inline spans

`content.go`: block text becomes a small `[]span{Text, Bold, Italic}` (parse `**…**` then `*…*`; unmatched markers stay literal). `firstHeading` and PPTX fill read the joined plain text. `docx.go` `appendDOCXBlocks` emits one run per span with `SetBold`/`SetItalic` (docxgo v2.14 supports both; `doc.go` examples). PPTX placeholder fill strips markers by joining span texts (run-level PPTX formatting stays a non-goal — its lib seam is per-paragraph). Nested/adjacent spans (`**a** *b*`) parse left-to-right; no markdown beyond bold/italic (lists/tables/etc. keep today's degrade-to-literal stance, stated in the guidance).

## D4: Guidance

One sentence appended to `createDocumentToolDescription`: DOCX skeletons may carry a `{{content}}` marker where the content belongs, and tables/code fences/images/horizontal rules are not supported. The documents skill playbook is unchanged (rule 4 already routes generation to create_document).

## D5: Non-goals

- PPTX run-level inline formatting.
- PDF path (already renders markdown through HTML).
- Any preset-schema/UI change — the marker is a skeleton-authoring convention, not a stored field.
- Rewriting existing user skeletons; the akta template keeps working, and with a `{{content}}` marker added it produces the intended fill.
