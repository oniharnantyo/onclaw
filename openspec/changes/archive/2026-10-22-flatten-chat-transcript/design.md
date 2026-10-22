# Design

## Context

See proposal.md — Why. The relevant current state:

- **The transcript surface is three nested boxes deep.** `ChatAssistantMessage.tsx` renders a reasoning group through the vendored `reasoning` element; `tools/aui/ToolCallFrame.tsx` renders every tool call on `surfaces.paper` (`bg-card border border-border/60`, `rounded-md`); `tools/aui/ChatToolGroup.tsx` wraps a run of one to three calls in the vendored `tool-group` element's `ghost` variant, whose trigger renders the literal text "1 tool call". The vendored reasoning root defaults to the `outline` variant (`rounded-lg border px-3 py-2`).
- **The run group hides the calls.** Radix's `Collapsible.Content` unmounts its children while closed, so the `ghost` group's `ToolGroupContent` — which holds the rendered cards — is not in the DOM at all. A probe of a one-call turn confirmed it: the transcript rendered the "1 tool call" trigger, and `querySelectorAll('[data-testid^="entry-tool-"]')` returned **zero** elements. The `chat-view` scenario "Tool call visible in transcript" (the transcript shows a tool-call card in message order whose header names the tool) therefore does not hold today for runs under the fold threshold.
- **The assistant's answer is the quietest thing on screen.** The message root is `max-w-[85%]` inside a `px-2` wrapper, so the prose is both narrower than the column and inset from it, while the surrounding cards are not.
- **The vendored element set already exposes the flat variants.** `reasoningVariants` has `outline` (default), `ghost`, and `muted`; `toolGroupVariants` has `outline`, `ghost`, and `muted`; the vendored `ReasoningRoot`, `ReasoningTrigger`, `ReasoningContent`, and `ReasoningText` all merge a caller-supplied `className` through `cn` (tailwind-merge), so a caller can override any conflicting utility. `surfaces.paper` is a shared constant, so it is not the seam for a per-surface change — but `ToolCallFrame` is a buddies-owned wrapper that merely *consumes* `paper`, and it can stop consuming it.
- **The rules that bound the change.** Design D13 and `frontend/src/components/assistant-ui/VENDORED.md`: the vendored tree is never hand-edited; buddies behaviour lives in the wrappers under `features/chat/`, and the one sanctioned exception is `elements/surfaces.tsx`. The repo's `frontend/AGENTS.md` adds: "Never hand-roll a transcript, tool card or composer element that the vendored set already covers."

## Goals / Non-Goals

**Goals:**

- One flat reading surface: the answer's prose and the agent's activity rows share a left edge, a measure, and a type scale.
- Every behavior the transcript has today — the reasoning disclosure, the per-call disclosure, the bodies, the fold at four or more calls, the approval split, the collapse map, the auto-open — survives the restyle unchanged.
- The vendored tree stays byte-identical, so re-vendoring remains a clean `add -o` plus the recorded `surfaces.tsx` retarget.

**Non-Goals:**

- No new feature: no turn summary, no elapsed-time header, no collapsing of a turn, no re-theming of the composer, the right sidebar, the run surfaces, or the right panel.
- No change to the tool bodies' own surfaces (the diff's grid, the terminal block, the code preview): those are content, not chrome, and the diff in particular needs a surface to read as a diff.
- No change to the data flow, the store, the reducer, the runtime adapter, the session model, or the bindings.

## Decisions

### D1. The flat grammar is expressed through the vendored elements' own props, never by editing them

`ReasoningRoot` is used with `variant="ghost"` (no border, no fill) and with caller `className` overrides on the root, the trigger, the content, and the text. `ToolCallFrame` stops consuming `surfaces.paper` and paints its own row. `ChatToolGroup` stops wrapping a short run in the vendored `tool-group` element and renders the calls directly.

*Why:* D13 and `VENDORED.md` make the vendored tree read-only, and the vendored elements already ship the flat variants and the `className` seams this change needs. Using them is also the only way the change stays re-vendorable: a future `npx assistant-ui add -o` regenerates the same files and the wrappers still compose them.

*Alternatives considered:* (a) editing the vendored `reasoning.tsx` to change its default variant — forbidden, and it would be lost on the next re-vendor; (b) adding a new vendored registry item with the flat look — a dependency and a re-vendor-recipe change for something the shipped elements already express; (c) hand-rolling a reasoning disclosure in the wrapper — rejected by `frontend/AGENTS.md` ("never hand-roll a transcript element the vendored set already covers").

### D2. A run under the fold threshold renders its calls inline; the fold at four or more is untouched

`ChatToolGroup` returns the group's rendered children directly when the run is shorter than `FOLD_MIN_STEPS` and holds no approval, instead of a `ghost` `ToolGroupRoot` whose collapsed content unmounts them.

*Why:* the current behavior contradicts `chat-view`'s "Tool call visible in transcript" (the card is not in the DOM at all), and it is the single loudest piece of the mess — a link standing where the work should be. The `chat-tool-cards` "Heavy-turn folding" requirement already fixes the fold threshold at four or more, so the sub-threshold case is free to render inline.

*Why not a different grouping:* the reference shows each call as its own line, in order, with no group header. The fold threshold, the summary row's wording, the trace rows, the per-file statistics, the approval split, and the failure tint all stay as they are.

### D3. The tool card's header is a quiet line whose payload is the brightest thing on it

The name and the sentence swap emphasis: the name recedes to the muted foreground, the sentence carries the payload (the path, the command, the file) in the full foreground, and the chips keep the mono treatment. The header keeps the disclosure control, the icon, the name, the sentence, the latency, and the failure chip; the body keeps the labeled arguments and result sections, the ten-line clamp, the raw toggle, and the per-tool shapes.

*Why:* in the reference the label is quiet and the payload is what the reader scans for. Today the name is the brightest element and the sentence is muted, so the eye lands on the label and has to work to find what was actually done.

*Failure:* the card loses its bordered surface entirely, so failure is carried by the existing `failed` chip and by the danger treatment on the name, not by a red border. `chat-tool-cards` "Tool call states" is restated to say so.

### D4. The body is revealed under a hairline rail, inset from the header

The body's container loses its top border and gains a left rail (`border-s`) with the content inset, so an expanded call reads as belonging to the line above it rather than as a second panel. The rail sits on the column's left edge — the same edge the answer's prose starts at — and the body's content is inset from it.

*Why:* the reference's expanded output is inset under the row it belongs to. The `border-t` divider is what makes the current card read as a box within a box.

### D5. The reasoning group is a disclosure line over a recessed aside

The root drops its border and fill; the trigger is a single muted line carrying the brain glyph, the word "Reasoning", the elapsed time when the group has one, and the chevron; the content is the vendored `ReasoningText` region, which already bounds its height and scrolls, with the text dimmed and inset from the line.

*Why:* the current card gives a thought the same visual weight as the answer it precedes, and the reasoning's own text is set in the same size and brightness as the answer's. The vendored `ReasoningText` already supplies the bounded scroll region the reference shows, so only the surface and the emphasis change.

*Honest limit:* the group carries an elapsed time only when the runtime gave its parts one. The mapping stamps timing on tool-call parts, not on reasoning parts, so a reasoning group has no duration today and the line shows none. Nothing is fabricated to fill the gap; `chat-view` states the elapsed time is shown "when the group carries one".

### D6. Turns are separated by a rule, and the answer shares the column's measure

The assistant message opens with a hairline rule above its content, and the message root drops both the `max-w-[85%]` cap and the wrapper's `px-2` inset, so the prose, the reasoning line, and the tool rows all start at the same left edge and span the same measure.

*Why:* the transcript currently gives the reader no way to tell where one turn's work ends and the next begins, and the answer is inset from the rows around it. The rule is the reference's own device for the first of the two.

**The vertical rhythm:** the reasoning line and the run of tool calls each carry air above and below them (`my-2`), so neither is glued to the prose; the rows INSIDE a run stay tight with each other (the reference reads them as consecutive lines). The run's own margin wraps the block, so the rows' margins are zeroed inside it (`[&>*]:my-0`) — the block breathes, the list stays a list.

### D7. The restyle is verified by the existing behavioral suites plus a small number of new chrome assertions

The suites that pin behavior — the disclosure mechanics, the bodies, the fold threshold, the approval split, the collapse map, the theme parity, the day dividers, the windowing — are unchanged and must stay green. The suites that pinned the *old* chrome (`toolCallFrame.test.tsx`'s `bg-card`/`border` assertions and its `border-danger/35` assertion, `toolRun.test.tsx`'s `border-danger/35` assertion) are retargeted at the new grammar, and the "a three-call run does not fold" scenario is retargeted from "a group trigger exists" to "the calls are in the transcript flow".

*Why:* those assertions exist to keep the card's surface in the two themes, not to freeze a particular border. Retargeting them keeps the intent (the surface is theme-correct) while letting the surface change. The `chat-view` delta's new scenarios are the durable statement of the new grammar.

## Risks / Trade-offs

- **A vendored element's internals change under a future re-vendor** → the flat look rides `variant="ghost"`, `className` merges, and the `data-slot` hooks the vendored files already expose; `VENDORED.md` records the recipe. The wrappers assert on `data-slot` and on the buddies-owned test ids, not on the vendored class names, so a re-vendor that renames a vendored class does not break the suites.
- **Removing the ghost group changes the DOM shape of short runs** → the `chat-view` delta states the new contract, and the affected scenario is rewritten to assert the calls are present.
- **Losing the card's surface makes the failure state less obvious** → the failure chip and the danger-tinted name carry it, and `chat-tool-cards` "Tool call states" is restated to name them; the `failed` chip and the `data-tool-state` attribute stay.
- **A flat transcript can read as undifferentiated** → the reasoning line, the tool name, the sentence, and the rule are the differentiators; the type scale and the muted-vs-bright split are what carry the hierarchy, exactly as in the reference.
- **The reasoning group has no elapsed time** → stated in the spec ("when the group carries one"); stamping reasoning timing would need a mapping change and a source of truth for a streamed thought's duration, which is a separate change.

## Migration Plan

Single frontend change, no backend, no bindings, no dependency change, no persistence change. Rollback is a revert of the wrapper files and the spec deltas.

## Open Questions

- Whether the reasoning group's elapsed time should be derived from the store's entry timestamps — the entries carry an arrival time that is the *start* of a streamed thought, not its end, so the number would understate. Deferred: the line simply carries no duration.
- Whether a turn-level summary line (the reference's "Worked for 5m 22s") is worth a turn-collapse interaction — a separate change; it would need a turn-level elapsed time and a viewport anchor.
