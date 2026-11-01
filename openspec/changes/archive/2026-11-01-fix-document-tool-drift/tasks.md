# Tasks

## 1. Implementation

- [x] 1.1 `internal/agents/docgen_tool.go`: append the production-routing sentence to `createDocumentToolDescription` (document files are produced through this tool; never shell-scripted generators or hand-assembled containers).
- [x] 1.2 `internal/agents/builtin_tools.go`: extend the read-file catalog description with the office-views clause (DOCX/XLSX/PPTX present as structured views — read them here, not by unpacking with shell).
- [x] 1.3 `internal/agents/contract.go`: add one operating-contract line covering both routings (produce documents with create_document; read office files with read_file).

## 2. Tests

- [x] 2.1 Contract tests: the composed contract contains the routing line; the create_document description states the production route; the read-file description states the office-views route.
- [x] 2.2 Existing description/appendix assertions updated where they pin exact strings.

## 3. Verification

- [x] 3.1 `gofmt -w .` && `go vet ./...` && `go test ./internal/agents/...`.
- [ ] 3.2 Live smoke (manual, Jono or equivalent): re-run the akta request; confirm the turn routes generation through create_document and reads the uploaded example via read_file instead of shell unpacking.
