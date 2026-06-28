# agent-core

## Purpose

Run a real, streaming, tool-calling ReAct agent loop over a remote LLM within a bounded context, with clean cancellation and a minimal transcript.

## ADDED Requirements

### Requirement: The agent runs a tool-calling ReAct loop over a remote ChatModel

The system SHALL run the agent as an eino ADK `ChatModelAgent` that reasons, invokes tools,
and produces an answer from a `model.ChatModel` built from the effective provider profile
resolved from the selected agent (cross-ref `agent-profiles`). `onclaw run`
SHALL submit a one-shot prompt and stream the result; `onclaw chat` SHALL run an interactive
read-eval-print loop, one turn per line of input.

#### Scenario: A one-shot prompt streams an answer

- **WHEN** a user runs `onclaw run "summarize README.md"` with a configured provider
- **THEN** the agent reasons, may call tools, and streams a final answer to stdout

#### Scenario: An interactive chat runs one turn per line

- **WHEN** a user runs `onclaw chat` and types a prompt followed by Enter
- **THEN** the agent completes that turn before reading the next line

### Requirement: Assistant tokens stream to the user as they arrive

The system SHALL stream model tokens to stdout as they are produced and SHALL NOT buffer the
full response before printing.

#### Scenario: Output appears incrementally

- **WHEN** the model emits streamed content
- **THEN** tokens are written to stdout progressively, not deferred to end-of-turn

### Requirement: The agent stays within the configured context budget

The system SHALL keep the conversation within `max_context_tokens` (default 8192) by
summarizing/compacting history before it exceeds the limit. The summarization SHALL trigger
well below the hard limit (around ~6000 tokens) so a turn never overruns the budget.

#### Scenario: A long conversation is compacted

- **WHEN** accumulated history approaches the trigger threshold
- **THEN** older messages are summarized and recent messages are retained, and the turn completes without exceeding `max_context_tokens`

### Requirement: Cancellation exits cleanly without a torn turn

The system SHALL propagate a cancellation signal (Ctrl-C, `/stop`, or a cancelled context)
into the running turn. On cancellation the loop SHALL stop promptly, write an `interrupted`
event to the transcript, and return without panicking or leaving a half-written turn.

#### Scenario: Ctrl-C mid-stream is clean

- **WHEN** the user interrupts a streaming turn
- **THEN** the turn stops, an `interrupted` line is recorded, and the process exits cleanly with no partial assistant message presented as complete

### Requirement: The agent records a minimal append-only transcript

The system SHALL append one JSON object per event to a per-session `.jsonl` transcript
(`user`, `assistant`, `tool_call`, `tool_result`, `interrupted`, `error`) and SHALL `fsync`
at turn boundaries. The full session SHALL NOT be held in memory. The transcript SHALL NOT
contain resolved secret values (cross-ref `providers`).

#### Scenario: A turn is persisted as events

- **WHEN** a turn runs that calls a tool and returns an answer
- **THEN** the transcript file contains matching `user`, `tool_call`, `tool_result`, and `assistant` lines in order

### Requirement: An agent gets the onboarding prompt until onboarding is completed

The system SHALL prepend an onboarding prompt (a markdown file — cross-ref `onboarding`) to
the system prompt while onboarding is incomplete — tracked by an `onboarding_completed`
preference that is unset/false until onboarding concludes. The onboarding prompt tells the
agent it has just woken up, encourages it to ask the user questions, and instructs it to
record what it learns by editing its persona files with its normal file tools (cross-ref
`agent-tools`) — no special tool is required. Once onboarding is marked complete, the system
SHALL stop injecting the onboarding prompt and assemble normally. (Persona files are seeded
from templates, so emptiness can no longer be the trigger — cross-ref `agent-identity`.)

#### Scenario: A run while onboarding is incomplete gets the prompt

- **WHEN** the master agent runs and `onboarding_completed` is unset
- **THEN** its system prompt includes the onboarding prompt; it asks the user questions and edits its persona files

#### Scenario: After onboarding completes, normal assembly resumes

- **WHEN** the agent runs after `onboarding_completed` has been set
- **THEN** the onboarding prompt is not injected and the agent assembles normally
