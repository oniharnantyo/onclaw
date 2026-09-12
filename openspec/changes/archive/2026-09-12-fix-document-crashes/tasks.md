## 1. Connector predicate

- [x] 1.1 Add the connector file-block predicate in `internal/agents/model_factory.go` (or a sibling): returns false for openai, openrouter, openai-compatible; true for anthropic, anthropic-compatible, gemini. Unit-test all six provider types.
- [x] 1.2 Thread the predicate into the runner's input-modality resolution (`runner.go`) alongside the existing catalog-based resolver, preserving `WithInputModalityResolver` test seams.

## 2. Live gate

- [x] 2.1 In `attachments_message.go`, gate the inline-PDF lane on the connector predicate: when the connector cannot accept file blocks, take `degradedAttachmentNote` regardless of catalog capability; the image lane is unchanged.
- [x] 2.2 Tests: PDF degrades to a pointer note on each OpenAI-family provider type (including unknown catalog capability); PDF stays a byte-carrying block on anthropic/gemini with a PDF-capable model; image lane behavior is byte-identical to today in all tri-states.

## 3. Summarize-window expansion

- [x] 3.1 Extract the stale-attachment expansion (`isStaleAttachmentBlock` + `attachmentPlaceholderText` application) into a reusable helper shared by the turn middleware and compaction; behavior of the turn path unchanged.
- [x] 3.2 In `compact.go`, apply the expansion to the window loaded by `loadSessionWindow` before `mw.Summarize`; expansion must not mutate the persisted log (conversion-time only).
- [x] 3.3 Tests: compact on a session whose history contains a reference-form PDF block succeeds on an OpenAI-family provider config; stale URL-only image blocks are expanded; the persisted session events are byte-identical before/after compaction.

## 4. Verification

- [x] 4.1 `go build ./...` and `go vet ./...` clean; touched suites green.
- [ ] 4.2 Manual pass: upload a PDF to an OpenAI-family agent — turn completes with a pointer note; `/compact` the session — summary generated, no converter error.
