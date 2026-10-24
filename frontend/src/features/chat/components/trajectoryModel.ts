/**
 * The delegated task trajectory's pure model (design D4, spec
 * merge-right-bar-previews "Subagent trajectory preview"): one JSONL record
 * per line of the child's materialized transcript file, parsed into the
 * ordered step list the trajectory tab renders. Total on any input — blank,
 * malformed, or unknown-step lines are skipped, never thrown — so a partially
 * written file renders the steps recorded so far. Records only append, so a
 * full re-parse of a grown file is idempotent: the re-parse of the shared
 * prefix yields identical steps and the result only ever grows.
 */
import { isObject, isString, type Json } from "../tools/parse";

/** The five transcript record kinds the runtime materializes (design D4). */
export type TrajectoryStepKind =
  | "task"
  | "text"
  | "reasoning"
  | "tool"
  | "result";

/** The two flattened tool-line shapes the runtime materializes: an
 * assistant-side call (`call <name> <args-json>`) and a user-side result
 * (`result <name>: <text>`). */
export type ToolPhase = "call" | "result";

/** One transcript record: a step kind plus its text payload. Tool records
 * carry their flattened line decomposed into phase, tool name, and the
 * payload side (raw JSON argument text, or the result text). */
export interface TrajectoryStep {
  kind: TrajectoryStepKind;
  content: string;
  /** Tool steps only: which flattened line shape the record carries. */
  phase?: ToolPhase;
  /** Tool steps only: the tool name both line shapes lead with. */
  toolName?: string;
  /** A call record's raw JSON argument text (after `call <name> `). */
  argsText?: string;
  /** A result record's text (after `result <name>: `). */
  resultText?: string;
}

const STEP_KINDS: readonly string[] = [
  "task",
  "text",
  "reasoning",
  "tool",
  "result",
];

/** Type guard: a record's `step` field naming one of the five kinds. */
function isStepKind(value: Json): value is TrajectoryStepKind {
  return isString(value) && STEP_KINDS.includes(value);
}

/** Decomposes one flattened tool line into its phase and payload sides; an
 * unrecognized shape keeps the verbatim line only. */
function parseToolLine(content: string): TrajectoryStep {
  if (content.startsWith("call ")) {
    const rest = content.slice("call ".length);
    const sep = rest.indexOf(" ");
    const toolName = sep === -1 ? rest : rest.slice(0, sep);

    return {
      kind: "tool",
      content,
      phase: "call",
      toolName,
      argsText: sep === -1 ? "" : rest.slice(sep + 1),
    };
  }

  if (content.startsWith("result ")) {
    const rest = content.slice("result ".length);
    const sep = rest.indexOf(": ");
    const toolName = sep === -1 ? rest : rest.slice(0, sep);

    return {
      kind: "tool",
      content,
      phase: "result",
      toolName,
      resultText: sep === -1 ? "" : rest.slice(sep + 2),
    };
  }

  return { kind: "tool", content };
}

/**
 * Parses a trajectory file's raw content into its steps, in file order.
 * Lines are trimmed and blank lines dropped; a line that fails to parse, or
 * whose `step` is not one of the five kinds, is skipped; a missing or
 * non-string `content` reads as "". Idempotent on append-only growth: re-
 * parsing a larger file returns the shared prefix's steps unchanged plus the
 * appended ones.
 */
export function parseTrajectory(raw: string): TrajectoryStep[] {
  const steps: TrajectoryStep[] = [];

  for (const line of raw.split("\n")) {
    const trimmed = line.trim();

    if (trimmed === "") continue;

    let parsed: Json;

    try {
      // SAFETY: the try/catch bounds the parse; the isObject guard below
      // establishes the record contract before the fields are read.
      parsed = JSON.parse(trimmed) as Json;
    } catch {
      continue;
    }

    if (!isObject(parsed)) continue;

    const kind = parsed.step;

    if (!isStepKind(kind)) continue;

    const content = parsed.content;

    if (kind === "tool") {
      steps.push(parseToolLine(isString(content) ? content : ""));
      continue;
    }

    steps.push({ kind, content: isString(content) ? content : "" });
  }

  return steps;
}

/**
 * One render block of the trajectory timeline: a parsed step, with a call
 * record and its paired result folded into a single tool block (design D4) —
 * the transcript renders one card per tool call, running until the result
 * arrives. The final `result` record (the child's answer) is not a block:
 * the parent transcript relays it, so the preview shows only the trajectory.
 */
export type TrajectoryBlock =
  | {
      kind: "task" | "text" | "reasoning";
      id: string;
      content: string;
    }
  | {
      kind: "tool";
      id: string;
      toolName: string;
      argsText: string;
      /** The paired result's text; absent until the result record lands. */
      result?: string;
      /** True while the call has no paired result yet. */
      pending: boolean;
    };

/**
 * Folds parsed steps into render blocks. A `call` result pairs with the most
 * recent unmatched call of the same tool name (the child writes the pair in
 * order); a result with no open call — a truncated transcript — becomes its
 * own completed block; an unrecognized tool line renders verbatim as its own
 * block. The final `result` record is dropped (see TrajectoryBlock). Block
 * ids derive from the record's file position, so re-parsing a grown file
 * keeps every shared block's id stable.
 */
export function trajectoryBlocks(
  steps: readonly TrajectoryStep[]
): TrajectoryBlock[] {
  const blocks: TrajectoryBlock[] = [];

  // Open call blocks by tool name; the arrays act as stacks (the child's
  // calls and results nest in order, so the last open call of a name closes
  // first). Only tool blocks ever enter the stacks.
  const open = new Map<string, Extract<TrajectoryBlock, { kind: "tool" }>[]>();

  for (let i = 0; i < steps.length; i++) {
    const step = steps[i];

    if (step.kind !== "tool") {
      if (step.kind === "result") continue;

      blocks.push({ kind: step.kind, id: `s${i}`, content: step.content });
      continue;
    }

    if (step.phase === "call") {
      const block: TrajectoryBlock = {
        kind: "tool",
        id: `s${i}`,
        toolName: step.toolName ?? "",
        argsText: step.argsText ?? "",
        pending: true,
      };

      const stack = open.get(block.toolName);

      if (stack) stack.push(block);
      else open.set(block.toolName, [block]);

      blocks.push(block);
      continue;
    }

    if (step.phase === "result") {
      const name = step.toolName ?? "";
      const stack = open.get(name);
      const match = stack?.pop();

      if (match !== undefined) {
        match.result = step.resultText ?? "";
        match.pending = false;

        if (stack !== undefined && stack.length === 0) open.delete(name);

        continue;
      }

      blocks.push({
        kind: "tool",
        id: `s${i}`,
        toolName: name,
        argsText: "",
        result: step.resultText ?? "",
        pending: false,
      });
      continue;
    }

    blocks.push({
      kind: "tool",
      id: `s${i}`,
      toolName: "",
      argsText: step.content,
      pending: false,
    });
  }

  return blocks;
}
