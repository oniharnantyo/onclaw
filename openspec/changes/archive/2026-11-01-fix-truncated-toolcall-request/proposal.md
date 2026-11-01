# Proposal

## Why

A provider-side output truncation mid-tool-call permanently kills the turn. Observed live (Z.AI Anthropic-compatible endpoint, agent with unset cap → 4096 default): a `create_document` call carrying a full deed draft exceeded the output budget, the stream stopped mid-arguments, eino's tool node failed with `unexpected EOF` — and the next model request 400'd with `Invalid request body: body.8176778: JSON decode error`, because the connector splices the history tool-call's arguments into the request as raw JSON (`agenticclaude` convertor.go: `input = json.RawMessage(call.Arguments)`) and a truncated fragment makes the whole request body malformed. Every retry re-sends the same fragment, so the session is bricked until manual intervention.

## What Changes

- Sanitize outbound requests at the runtime's model seam: before every Generate/Stream, each history tool call whose arguments are non-empty but not valid JSON is rewritten to `{}` on a copied block, so a truncated call can never make the request body unencodable. The tool-result error the model already received ("failed to unmarshal arguments…") is what tells it to re-issue.
- Raise the Claude-family default output cap from 4096 to 16384: at 4096, a realistic document-producing tool call truncates, and post-fix recovery would re-truncate on every retry (a loop bounded only by the iteration cap). **Reviewable product call** — revert this bullet if the cost profile matters more.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-runtime`: the Provider registry requirement gains request-encoding robustness — a history tool call with unencodable arguments MUST be sent in a structurally valid form, never echoed back verbatim.

## Impact

- New decorator in `internal/agents` (wraps `model.BaseModel[*schema.AgenticMessage]`, applied where the registry builds the model) plus unit tests; one constant change (`DefaultMaxTokens`) and its test updates.
- Non-goals: the ~8MB request bodies themselves (accumulated big tool outputs / base64 attachments — compaction already owns that); no change to how truncation is surfaced to the user (completed partial turn with the resume affordance stays); no vendored-connector edits.
