# Tasks

## 1. Implementation

- [x] 1.1 `internal/agents/registry.go`: pass `agent.MaxTokens` raw through `buildModel` (drop the `<= 0 → DefaultMaxTokens` substitution there).
- [x] 1.2 `internal/agents/connectors.go`: Claude builder takes over the `<= 0 → DefaultMaxTokens` substitution (thinking-budget headroom unchanged); openai-family builder sets `MaxCompletionTokens` when the raw cap is `> 0`; Gemini builder sets the genai client's max-output-tokens when `> 0` — first confirm the field on the pinned genai version (`go doc google.golang.org/genai.ClientConfig`).
- [x] 1.3 Update the `DefaultMaxTokens` doc comment (`internal/agents/registry.go`) to say it is the Claude-family required-value default.

## 2. Tests

- [x] 2.1 Registry tests: raw value reaches every family builder; unset reaches openai/Gemini as zero and Claude as 4096.
- [x] 2.2 Connector tests: openai-family config carries `MaxCompletionTokens` only when pinned; Gemini carries max-output-tokens only when pinned; Claude unchanged (default substitution + thinking headroom).

## 3. Verification

- [x] 3.1 `gofmt -w .` && `go vet ./...` && `go test ./internal/agents/...`.
- [ ] 3.2 Live smoke (env-gated or manual): an agent with a small pinned cap on an openai-compatible provider truncates early; with the cap unset, behavior matches today's provider default; a long-reasoning Z.AI turn no longer dies mid-sentence at the default budget when the agent pins a generous cap.
