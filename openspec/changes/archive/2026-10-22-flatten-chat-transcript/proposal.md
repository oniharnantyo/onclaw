# Proposal

## Why

The chat transcript reads as a stack of nested boxes rather than a conversation. Every reasoning block is a bordered rounded card (`aui-reasoning-root` at the `outline` variant), every tool call is a second bordered card (`bg-card border border-border/60`) nested inside a third, and the run of one to three calls that a turn usually makes is hidden behind a "1 tool call" ghost disclosure whose content Radix unmounts while closed — so the reader sees a link where the work should be. The result is that the agent's actual output (its prose) is visually quieter than the scaffolding around it, and the transcript gives the reader no way to scan a turn: no boundary between turns, no hierarchy between an answer and the machinery that produced it, and no flat line to skim.

The reference UI this is measured against solves the same problem with a flat, quiet grammar: a turn is a section under a rule, a thought is one muted line that opens into an indented aside, a tool call is one muted line that names the tool and the target, and only the answer is set in full-voice prose. The current surface already owns every behavior the reference has (the disclosure mechanics, the per-tool bodies, the fold for long runs, the approvals) — what it lacks is the grammar that presents them.

## What Changes

- **Reasoning becomes a quiet one-line disclosure.** The vendored reasoning element keeps its collapsible mechanics, its streaming preview, and its scroll lock; the box around it goes. A thought reads as a single muted line (icon, label, elapsed time, chevron) and opens into an indented, recessed aside with a bounded scroll region — a quiet aside, not a panel competing with the answer.
- **Tool calls render inline, one line each.** A run of fewer than four consecutive calls no longer collapses behind a "N tool calls" disclosure: each call appears in the transcript flow, in order, so the reader sees what the agent did without expanding anything. The call's own disclosure still reveals its body, and the fold for four or more calls is unchanged.
- **A tool call loses its card surface.** The header becomes a quiet line — icon, tool name, the one-line sentence, the latency — with the body revealed beneath it under a hairline rail. The name recedes and the payload (the path, the command, the file) reads brightest, so the reader's eye lands on what happened rather than on the label.
- **Turns become sections.** Each assistant turn is preceded by a hairline rule, so the transcript reads as a sequence of turns instead of one undifferentiated column.
- **The assistant's prose owns the measure.** The answer and the rows above it share one left edge and one column width; the extra inset and the 85% cap that squeezed the answer away from the tool rows are gone.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `chat-view`: "Chat transcript rendering" gains the flat visual grammar — reasoning and tool activity render as quiet one-line disclosures in the message flow rather than as bordered cards, a run of fewer than four calls renders inline instead of behind a group disclosure, each assistant turn is separated by a rule, and the answer shares the column's measure with the rows above it.
- `chat-tool-cards`: "Tool card anatomy" is restated — the card's header is a quiet line with no card surface of its own, its name recedes while the sentence carries the payload, and its body is revealed beneath the header under a hairline rail; "Tool call states" keeps every behavior and states that failure is carried by the failure chip and the danger treatment rather than by a bordered surface.

## Impact

- **Frontend view layer** (`frontend/src/features/chat/`): the reasoning group in `thread/ChatAssistantMessage.tsx`, the tool frame in `tools/aui/ToolCallFrame.tsx`, the run group in `tools/aui/ChatToolGroup.tsx`, and the prompt surface in `thread/ChatUserMessage.tsx` are restyled; the transcript's own chrome in `thread/ChatThread.tsx` gains the turn rule. No vendored file under `frontend/src/components/assistant-ui/` is edited — the restyle rides the vendored elements' existing `variant` and `className` props and the shared `collapsible-root` primitive, per the re-vendoring rule in `VENDORED.md`.
- **Tests**: the suites that pin the current chrome (`toolCallFrame.test.tsx`, `toolRun.test.tsx`) are updated to pin the new grammar; the suites that pin behavior (the disclosure mechanics, the fold threshold, the approval split, the bodies, the theme parity) are unchanged and keep passing.
- **No backend, no bindings, no dependency change**: the transcript's data flow, the store, the reducer, the runtime adapter, and the session model are untouched.
