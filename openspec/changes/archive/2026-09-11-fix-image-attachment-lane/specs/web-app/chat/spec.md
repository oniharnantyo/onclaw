## ADDED Requirements

### Requirement: Capability-aware attachment hint
When the user attaches an image or PDF file to a chat whose agent's model does not accept that input kind, the attachment chip SHALL display a soft warning naming the degraded behavior (e.g. "this model can't see images — will attach as reference only"). The warning SHALL NOT block sending. The hint SHALL appear only when capability is affirmatively unsupported; a model with unknown capability shows no hint. The agent's input-modality capability SHALL reach the chat client through the agent data the composer already loads.

#### Scenario: Image chip warns on a text-only model
- **WHEN** an image chip lands in the composer of a chat with an agent whose model is affirmatively text-only
- **THEN** the chip shows the degraded-behavior warning and the send button remains enabled

#### Scenario: No warning for capable or unknown models
- **WHEN** an image chip lands in a chat whose agent's model supports image input, or whose capability is unknown
- **THEN** the chip renders without the warning
