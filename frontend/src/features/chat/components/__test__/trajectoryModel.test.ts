/**
 * Trajectory model tests (design D4, spec merge-right-bar-previews "Subagent
 * trajectory preview"): the parser yields each of the five step kinds in
 * file order — tool lines decomposed into their call/result phase, tool name,
 * and payload side — skips blank, malformed, and unknown-step lines without
 * throwing, and is idempotent under append-only growth. The block fold pairs
 * each call with its result into one tool block, the transcript's one-card-
 * per-call shape.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  parseTrajectory,
  trajectoryBlocks,
  type TrajectoryStep,
} from "../trajectoryModel";

/** One JSONL transcript record. */
function line(step: string, content: string): string {
  return JSON.stringify({ step, content });
}

describe("parseTrajectory", () => {
  it("parses each of the five step kinds in file order", () => {
    const raw = [
      line("task", "Research the retry policy"),
      line("reasoning", "I should check the docs first"),
      line("tool", "call read_file docs/retry.md"),
      line("tool", "result read_file: # Retry policy"),
      line("text", "The policy retries **three** times."),
      line("result", "Retries are capped at three."),
    ].join("\n");

    expect(parseTrajectory(raw)).toEqual([
      { kind: "task", content: "Research the retry policy" },
      { kind: "reasoning", content: "I should check the docs first" },
      {
        kind: "tool",
        content: "call read_file docs/retry.md",
        phase: "call",
        toolName: "read_file",
        argsText: "docs/retry.md",
      },
      {
        kind: "tool",
        content: "result read_file: # Retry policy",
        phase: "result",
        toolName: "read_file",
        resultText: "# Retry policy",
      },
      { kind: "text", content: "The policy retries **three** times." },
      { kind: "result", content: "Retries are capped at three." },
    ] satisfies TrajectoryStep[]);
  });

  it("splits a call's arguments at the first space, keeping JSON intact", () => {
    const [step] = parseTrajectory(
      line(
        "tool",
        'call read_file {"file_path": "/workspace/SOUL.md", "limit": 100}'
      )
    );

    expect(step.phase).toBe("call");
    expect(step.toolName).toBe("read_file");
    expect(step.argsText).toBe(
      '{"file_path": "/workspace/SOUL.md", "limit": 100}'
    );
  });

  it("splits a result at the first colon-space, keeping later colons in the text", () => {
    const [step] = parseTrajectory(
      line("tool", "result http_get: status: 200, body: ok")
    );

    expect(step.phase).toBe("result");
    expect(step.toolName).toBe("http_get");
    expect(step.resultText).toBe("status: 200, body: ok");
  });

  it("degrades a tool line with no payload side to an empty one", () => {
    expect(parseTrajectory(line("tool", "call ls"))).toEqual([
      {
        kind: "tool",
        content: "call ls",
        phase: "call",
        toolName: "ls",
        argsText: "",
      },
    ]);
  });

  it("defaults a missing or non-string content to an empty string", () => {
    expect(parseTrajectory('{"step":"text"}')).toEqual([
      { kind: "text", content: "" },
    ]);
    expect(parseTrajectory('{"step":"tool","content":42}')).toEqual([
      { kind: "tool", content: "" },
    ]);
  });

  it("skips blank, malformed, and unknown-step lines without throwing", () => {
    const raw = [
      "",
      "   ",
      "not json at all",
      '{"step":"text","content":"kept"}',
      "{broken",
      '{"step":"musing","content":"not a kind"}',
      '["not","an","object"]',
      "42",
    ].join("\n");

    expect(parseTrajectory(raw)).toEqual([{ kind: "text", content: "kept" }]);
  });

  it("parses an empty or whitespace-only file to no steps", () => {
    expect(parseTrajectory("")).toEqual([]);
    expect(parseTrajectory("\n \n\t\n")).toEqual([]);
  });

  it("re-parsing a grown file is a superset with an identical prefix", () => {
    const original = [
      line("task", "Research the retry policy"),
      line("tool", "call read_file docs/retry.md"),
      line("text", "The policy retries **three** times."),
    ].join("\n");

    const before = parseTrajectory(original);

    // The runtime appends the next records; the earlier bytes are unchanged.
    const grown = `${original}\n${line("result", "Retries are capped at three.")}`;

    const after = parseTrajectory(grown);

    expect(after.length).toBe(before.length + 1);
    expect(after.slice(0, before.length)).toEqual(before);
    expect(after[after.length - 1]).toEqual({
      kind: "result",
      content: "Retries are capped at three.",
    });
    expect(parseTrajectory(grown)).toEqual(after);
  });
});

describe("trajectoryBlocks", () => {
  it("folds a call and its result into one completed tool block", () => {
    const steps = parseTrajectory(
      [
        line("task", "Summarize the docs"),
        line("tool", 'call read_file {"file_path": "docs/retry.md"}'),
        line("tool", "result read_file: # Retry policy"),
      ].join("\n")
    );

    expect(trajectoryBlocks(steps)).toEqual([
      { kind: "task", id: "s0", content: "Summarize the docs" },
      {
        kind: "tool",
        id: "s1",
        toolName: "read_file",
        argsText: '{"file_path": "docs/retry.md"}',
        result: "# Retry policy",
        pending: false,
      },
    ]);
  });

  it("leaves an unmatched call pending", () => {
    const steps = parseTrajectory(
      [line("tool", 'call read_file {"file_path": "a.md"}')].join("\n")
    );

    expect(trajectoryBlocks(steps)).toEqual([
      {
        kind: "tool",
        id: "s0",
        toolName: "read_file",
        argsText: '{"file_path": "a.md"}',
        pending: true,
      },
    ]);
  });

  it("pairs a result with the most recent open call of the same tool", () => {
    const steps = parseTrajectory(
      [
        line("tool", 'call read_file {"file_path": "a.md"}'),
        line("tool", 'call read_file {"file_path": "b.md"}'),
        line("tool", "result read_file: second"),
        line("tool", "result read_file: first"),
      ].join("\n")
    );

    const blocks = trajectoryBlocks(steps);

    expect(blocks).toHaveLength(2);
    expect(blocks[0]).toMatchObject({
      kind: "tool",
      id: "s0",
      argsText: '{"file_path": "a.md"}',
      result: "first",
      pending: false,
    });
    expect(blocks[1]).toMatchObject({
      kind: "tool",
      id: "s1",
      argsText: '{"file_path": "b.md"}',
      result: "second",
      pending: false,
    });
  });

  it("renders a result with no open call as its own completed block", () => {
    const steps = parseTrajectory(
      [line("tool", "result read_file: orphan")].join("\n")
    );

    expect(trajectoryBlocks(steps)).toEqual([
      {
        kind: "tool",
        id: "s0",
        toolName: "read_file",
        argsText: "",
        result: "orphan",
        pending: false,
      },
    ]);
  });

  it("keeps block ids stable when the file grows", () => {
    const original = parseTrajectory(
      [line("tool", 'call read_file {"file_path": "a.md"}')].join("\n")
    );

    const grown = parseTrajectory(
      [
        line("tool", 'call read_file {"file_path": "a.md"}'),
        line("tool", "result read_file: done"),
        line("text", "All finished."),
      ].join("\n")
    );

    const before = trajectoryBlocks(original);
    const after = trajectoryBlocks(grown);

    expect(before[0].id).toBe(after[0].id);
    expect(after).toHaveLength(2);
    expect(after[0]).toMatchObject({ pending: false, result: "done" });
    expect(after[1]).toMatchObject({ kind: "text", content: "All finished." });
  });

  it("renders an unrecognized tool line verbatim as its own block", () => {
    const steps = parseTrajectory(
      [line("tool", "mystery line without a shape")].join("\n")
    );

    expect(trajectoryBlocks(steps)).toEqual([
      {
        kind: "tool",
        id: "s0",
        toolName: "",
        argsText: "mystery line without a shape",
        pending: false,
      },
    ]);
  });

  it("drops the final result record — the parent relays the child's answer", () => {
    const steps = parseTrajectory(
      [
        line("task", "Summarize the docs"),
        line("text", "Reading now."),
        line("result", "Retries are capped at three."),
      ].join("\n")
    );

    expect(trajectoryBlocks(steps)).toEqual([
      { kind: "task", id: "s0", content: "Summarize the docs" },
      { kind: "text", id: "s1", content: "Reading now." },
    ]);
  });
});
