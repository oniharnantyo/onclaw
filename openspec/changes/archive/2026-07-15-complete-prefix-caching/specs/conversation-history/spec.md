# Capability: Conversation History

## MODIFIED Requirements

### Requirement: Prefix caching is enabled per provider where supported
The system SHALL enable conversation prefix caching for providers that support it, so the stable
replayed prefix is amortized across turns rather than re-billed in full. Caching SHALL be wired per
adapter (Anthropic cache control, OpenAI-compatible automatic prefix caching, Gemini context
caching) and SHALL degrade gracefully to re-billing where a provider offers no caching. The
cache-stability invariant (no per-turn mutation of replayed messages) SHALL be preserved so caching
remains effective.

#### Scenario: A cacheable prefix is reused across turns
- **WHEN** a provider supports prefix caching and a conversation replays a stable prefix across consecutive turns
- **THEN** the provider reports cache hits on the stable portion of the prefix

#### Scenario: A non-caching provider still functions
- **WHEN** a provider offers no prefix caching
- **THEN** the agent still replays and runs correctly, re-billing the prefix

#### Scenario: Gemini context caching is wired at runtime
- **WHEN** a Gemini profile has prefix caching enabled
- **THEN** a cached-content resource is created from the stable prefix and referenced on subsequent turns, so the prefix is cached rather than re-billed (until this is wired, Gemini degrades to re-billing — graceful)

#### Scenario: Anthropic cache breakpoint covers the stable prefix
- **WHEN** an Anthropic request is sent across consecutive turns
- **THEN** the cache breakpoint is placed at the stable system + tools + history prefix boundary (not only the final message block), so the prefix is explicitly cached
