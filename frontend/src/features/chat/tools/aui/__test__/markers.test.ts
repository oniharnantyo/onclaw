/**
 * markers.ts tests (task 4.1, design D7): each trailing marker parses into
 * its flag and leaves the body clean, timeout and truncation combine, a
 * failed exit combines with truncation, marker-less output passes through,
 * ANSI escapes strip defensively, and empty output yields an empty parse.
 */
import { describe, expect, it } from "vite-plus/test";

import { parseExecuteOutput } from "../markers";

describe("parseExecuteOutput (task 4.1)", () => {
  it("parses the failed-exit marker into its code", () => {
    const parsed = parseExecuteOutput(
      "some output\n[Command failed with exit code 3]"
    );

    expect(parsed.lines).toEqual(["some output"]);
    expect(parsed.exitCode).toBe(3);
    expect(parsed.timedOut).toBe(false);
    expect(parsed.truncated).toBe(false);
    expect(parsed.noOutput).toBe(false);
  });

  it("parses the timeout marker", () => {
    const parsed = parseExecuteOutput(
      "partial\n[Command timed out and was stopped]"
    );

    expect(parsed.lines).toEqual(["partial"]);
    expect(parsed.timedOut).toBe(true);
    expect(parsed.exitCode).toBeUndefined();
  });

  it("parses the truncation marker", () => {
    const parsed = parseExecuteOutput(
      "line\n[Output was truncated due to size limits]"
    );

    expect(parsed.lines).toEqual(["line"]);
    expect(parsed.truncated).toBe(true);
  });

  it("parses the no-output marker into an empty body", () => {
    const parsed = parseExecuteOutput(
      "[Command executed successfully with no output]"
    );

    expect(parsed.lines).toEqual([]);
    expect(parsed.noOutput).toBe(true);
  });

  it("parses timeout and truncation together", () => {
    const parsed = parseExecuteOutput(
      "head\n[Output was truncated due to size limits]\n[Command timed out and was stopped]"
    );

    expect(parsed.lines).toEqual(["head"]);
    expect(parsed.timedOut).toBe(true);
    expect(parsed.truncated).toBe(true);
  });

  it("parses a failed exit together with truncation", () => {
    const parsed = parseExecuteOutput(
      "boom\n[Output was truncated due to size limits]\n[Command failed with exit code 127]"
    );

    expect(parsed.lines).toEqual(["boom"]);
    expect(parsed.exitCode).toBe(127);
    expect(parsed.truncated).toBe(true);
  });

  it("passes marker-less output through untouched", () => {
    const text = "alpha\nbeta\n";

    const parsed = parseExecuteOutput(text);

    expect(parsed.lines).toEqual(["alpha", "beta"]);
    expect(parsed.exitCode).toBeUndefined();
    expect(parsed.timedOut).toBe(false);
    expect(parsed.truncated).toBe(false);
    expect(parsed.noOutput).toBe(false);
  });

  it("keeps a marker-shaped line that is not trailing in the body", () => {
    const parsed = parseExecuteOutput(
      "[Command executed successfully with no output]\nlater output"
    );

    expect(parsed.lines).toEqual([
      "[Command executed successfully with no output]",
      "later output",
    ]);
    expect(parsed.noOutput).toBe(false);
  });

  it("strips ANSI escape sequences defensively", () => {
    const parsed = parseExecuteOutput(
      "\u001B[32mok\u001B[0m\n\u001B[1;31mfatal error\u001B[0m\n[Command failed with exit code 1]"
    );

    expect(parsed.lines).toEqual(["ok", "fatal error"]);
    expect(parsed.exitCode).toBe(1);
  });

  it("parses empty output into an empty, unflagged result", () => {
    const parsed = parseExecuteOutput("");

    expect(parsed.lines).toEqual([]);
    expect(parsed.exitCode).toBeUndefined();
    expect(parsed.timedOut).toBe(false);
    expect(parsed.truncated).toBe(false);
    expect(parsed.noOutput).toBe(false);
  });
});
