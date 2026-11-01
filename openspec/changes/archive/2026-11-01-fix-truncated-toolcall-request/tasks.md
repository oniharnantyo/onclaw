# Tasks

## 1. Implementation

- [x] 1.1 New decorator (internal/agents, e.g. `sanitize_model.go`): wrap `model.BaseModel[*schema.AgenticMessage]`; sanitize Generate/Stream inputs per design D2 (copy-on-write blocks, `json.Valid` gate, `"{}"` replacement).
- [x] 1.2 Apply the decorator in `registry.Build` so every turn's model is wrapped.
- [x] 1.3 Raise the Claude-family `DefaultMaxTokens` 4096→16384 and update its doc comment.

## 2. Tests

- [x] 2.1 Decorator tests: truncated arguments (`{ "file_name": "x`) → `{}` reaches the base model, other blocks pointer-identical, valid arguments untouched, empty arguments untouched; Stream and Generate both covered; input slice not mutated in place.
- [x] 2.2 Update the maxtokens tests asserting the 4096 default to 16384.

## 3. Verification

- [x] 3.1 `gofmt -w .` && `go vet ./...` && `go test -count=1 ./internal/agents/...`.
- [ ] 3.2 Live smoke (manual): replay the akta `create_document` turn on the Z.AI agent — with a pinned large cap the call completes; force-truncation path (small pinned cap) ends in a recoverable tool error, not a 400 NodeRunError.
