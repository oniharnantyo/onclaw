# Tasks

## 1. Implementation

- [ ] 1.1 `content.go`: inline span parsing (`**bold**`, `*italic*`) — block text becomes `[]span`; unmatched markers stay literal; `firstHeading`/PPTX read joined plain text.
- [ ] 1.2 `docx.go`: `appendDOCXBlocks` emits one run per span with `SetBold`/`SetItalic`.
- [ ] 1.3 `docx.go`: `renderDOCX` marker placement — scan opened skeleton for the `{{content}}` paragraph; when present, replace it at position with the rendered content fragment (splice precedent per design D2); when absent, keep today's append.
- [ ] 1.4 `pptx.go`: placeholder fill joins span plain text (markers stripped).
- [ ] 1.5 `internal/agents/docgen_tool.go`: append the marker + unsupported-constructs sentence to `createDocumentToolDescription`.

## 2. Tests

- [ ] 2.1 Parser tests: bold/italic/adjacent/nested-marker cases; unmatched markers literal; heading/firstHeading unaffected.
- [ ] 2.2 DOCX writer tests: skeleton with marker → content at marker position, no marker paragraph in output, skeleton body preserved; skeleton without marker → append; bold/italic runs verified in the output XML.
- [ ] 2.3 PPTX test: placeholder text carries plain words without markers.
- [ ] 2.4 Description test: the new sentence is asserted with the routing tests from fix-document-tool-drift.

## 3. Verification

- [ ] 3.1 `gofmt -w .` && `go vet ./...` && `go test -count=1 ./internal/agents/...`.
- [ ] 3.2 Live smoke (manual): regenerate the akta draft via create_document with the user's preset — confirm no duplicated template body (after the user adds a `{{content}}` marker to their skeleton) and no literal `**` markers in the output.
