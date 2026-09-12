## MODIFIED Requirements

### Requirement: Model-modality attachment degradation
When building a turn's model message, the runtime SHALL resolve whether the turn's (provider, model) accepts each inline attachment input kind (image, PDF) and, for an input the model does not accept, SHALL replace that block with a pointer note naming the file, its type and size, and stating that the current model cannot view it — the run SHALL succeed. Capability is resolved per (provider, model), never by model name alone, and is tri-state: supported, unsupported, or unknown. Unknown capability SHALL fail open (the block is sent as-is). File blocks are additionally gated by connector support: for a provider whose model connector cannot convert file blocks, the runtime SHALL degrade the PDF block to a pointer note regardless of catalog capability — unknown connector support SHALL NOT fail open for file blocks. The image lane is unaffected by the connector gate.

#### Scenario: Image degrades on a text-only model
- **WHEN** an agent whose model accepts text-only input executes a turn carrying an inline-image attachment
- **THEN** the model message contains a pointer note for the image (name, type, size, cannot-view notice) instead of an image block, and the run completes successfully

#### Scenario: Image is sent whole on a vision-capable model
- **WHEN** an agent whose model accepts image input executes the same turn
- **THEN** the model message contains the image block with inline bytes and no pointer note

#### Scenario: Regeneration after a model switch sees the image again
- **WHEN** a turn that degraded on a text-only model is regenerated after the agent was reconfigured to a vision-capable model
- **THEN** the rebuilt model message contains the image block with inline bytes

#### Scenario: Unknown capability fails open
- **WHEN** the turn's (provider, model) has no catalog entry and no hint, and the turn carries an inline-image attachment
- **THEN** the image block is sent as-is (today's wire behavior), not degraded

#### Scenario: PDF degrades on a connector without file-block support
- **WHEN** an agent on a provider whose connector cannot convert file blocks (openai, openrouter, or openai-compatible) executes a turn carrying an inline-PDF attachment, regardless of the model's catalog PDF capability
- **THEN** the model message contains a pointer note for the PDF instead of a file block, and the run completes successfully without a converter error

#### Scenario: PDF stays native on a connector with file-block support
- **WHEN** an agent on a provider whose connector converts file blocks (anthropic or gemini) executes a turn carrying an inline-PDF attachment on a PDF-capable model
- **THEN** the model message contains the file block with inline bytes and no pointer note

#### Scenario: Unknown capability no longer fails open for PDF on the OpenAI family
- **WHEN** the turn's (openai-family provider, model) has no catalog entry and no hint, and the turn carries an inline-PDF attachment
- **THEN** the file block is degraded to a pointer note, not sent as-is

### Requirement: Context summarization
When an execution's working-context token count exceeds the resolved context window multiplied by a server-configured safety margin, the runtime SHALL compress the conversation history into a summary generated with the agent's own provider/model, SHALL offload the full pre-compaction history to `transcript.md` inside the agent's workspace directory, SHALL continue the execution with the compressed window plus the agent's recent user messages, and SHALL record the replacement in the session history so the full prior record remains retrievable by replay. The context-compacted transcript event — emitted live and on replay of the session log — SHALL carry the working-context token estimates before and after the compaction. The messages handed to the summarizer SHALL NOT contain raw attachment blocks: stale attachment blocks (reference-form images and files from earlier turns) SHALL first be expanded to the same model-facing placeholder text the turn path uses, so summarization succeeds on sessions that contain attachments.

#### Scenario: Trigger fires mid-conversation
- **WHEN** a long thread's token count crosses the resolved context window × margin
- **THEN** the next execution continues from a compressed window and `transcript.md` in the agent directory contains the full prior history

#### Scenario: Compaction is auditable
- **WHEN** a compaction has occurred on a thread
- **THEN** the session history contains a window-replacement record and replaying the full log still yields the pre-compaction messages

#### Scenario: Compaction event carries token estimates
- **WHEN** a context-compacted event is emitted live or replayed from the session log
- **THEN** it carries the working-context token estimates before and after the compaction

#### Scenario: Summarizer never receives raw attachment blocks
- **WHEN** compaction runs on a session whose persisted history contains reference-form image or PDF attachment blocks
- **THEN** the summarizer's model call receives placeholder text naming those attachments instead of the raw blocks, and the summary is generated successfully

### Requirement: Manual compaction command
A turn submitted with the compact command SHALL NOT run a normal model chat turn and SHALL NOT append a user message to the session. The runtime SHALL load the session's current message window, expand stale attachment blocks in that window to the model-facing placeholder text (as the turn path does), generate a summary with the agent's own provider/model under an instruction incorporating the command's optional focus text, offload the full pre-compaction history to `transcript.md` in the agent's workspace directory (the same retention rule as automatic compaction), record the window replacement in the session history, and emit the context-compacted transcript event — carrying before/after working-context token estimates — followed by a terminal turn-completed event carrying the summarizer call's usage. Manual compaction SHALL execute regardless of the current token count. A compact command against a session with no compactable message history SHALL complete without emitting a compaction event.

#### Scenario: Compact rewrites the window
- **WHEN** a compact command executes on a session with prior turns
- **THEN** the working window is replaced by the summary, `transcript.md` in the agent directory holds the full prior history, and the stream ends with the context-compacted event (token estimates included) followed by turn-completed carrying the summarizer usage

#### Scenario: Focus text shapes the summary instruction
- **WHEN** the compact command carries focus text
- **THEN** the summary is generated under an instruction that incorporates that text

#### Scenario: Below-threshold compaction allowed
- **WHEN** a compact command executes on a session whose token count is under the automatic trigger threshold
- **THEN** compaction still executes

#### Scenario: Empty history is a quiet no-op
- **WHEN** a compact command executes on a session with no compactable messages
- **THEN** the turn completes without error and without a compaction event

#### Scenario: Compact succeeds on a session containing a PDF attachment
- **WHEN** a compact command executes on a session whose history contains a PDF attachment turn, on any provider including the OpenAI family
- **THEN** the compaction completes with a summary and does not fail with a content-block conversion error
