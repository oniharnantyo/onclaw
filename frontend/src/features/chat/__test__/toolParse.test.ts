/**
 * parse.ts unit tests (task 3.1): both doors from the raw ToolActivity
 * strings are total — malformed payloads return null so bodies fall back to
 * the raw pane, never throw.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  bool,
  callDurationMs,
  num,
  parseArgs,
  parseJSONResult,
  str,
  type ToolArgs,
} from "../tools/parse";

describe("parseArgs", () => {
  it("parses a valid JSON object", () => {
    expect(parseArgs('{"file_path":"/workspace/a.md"}')).toEqual({
      file_path: "/workspace/a.md",
    });
  });

  it("returns an empty object for absent and blank arguments", () => {
    expect(parseArgs(undefined)).toEqual({});
    expect(parseArgs(null)).toEqual({});
    expect(parseArgs("")).toEqual({});
    expect(parseArgs("   ")).toEqual({});
  });

  it("returns null (not throw) on garbage", () => {
    expect(parseArgs("{not json")).toBeNull();
    expect(parseArgs("execute ls -la")).toBeNull();
    expect(parseArgs('{"a":')).toBeNull();
  });

  it("returns null for non-object JSON", () => {
    expect(parseArgs("[1,2]")).toBeNull();
    expect(parseArgs('"just a string"')).toBeNull();
    expect(parseArgs("42")).toBeNull();
    expect(parseArgs("null")).toBeNull();
  });

  it("never throws on any input", () => {
    expect(() => parseArgs('{"a": NaN}')).not.toThrow();
  });
});

describe("parseJSONResult", () => {
  it("parses shaped results", () => {
    expect(parseJSONResult('{"results":[]}')).toEqual({ results: [] });
    expect(parseJSONResult("[1,2]")).toEqual([1, 2]);
  });

  it("returns null for absent, blank, and non-JSON text", () => {
    expect(parseJSONResult(undefined)).toBeNull();
    expect(parseJSONResult(null)).toBeNull();
    expect(parseJSONResult("")).toBeNull();
    expect(parseJSONResult("Sunny, 21°C")).toBeNull();
    expect(parseJSONResult("exit status 1: boom")).toBeNull();
  });

  it("returns parsed strings as strings", () => {
    expect(parseJSONResult('"quoted"')).toBe("quoted");
  });
});

describe("defensive field readers", () => {
  // SAFETY: the fixture mixes field types on purpose (a number where a
  // string belongs); the readers under test must survive exactly that.
  const args = {
    s: "text",
    n: 3,
    b: true,
    badN: "nope",
  } as ToolArgs;

  it("str falls back on wrong-typed or missing fields", () => {
    expect(str(args, "s")).toBe("text");
    expect(str(args, "missing", "dflt")).toBe("dflt");
    expect(str(args, "n")).toBe("");
    expect(str(null, "s")).toBe("");
  });

  it("bool and num fall back on wrong-typed fields", () => {
    expect(bool(args, "b")).toBe(true);
    expect(bool(args, "missing", true)).toBe(true);
    expect(bool(args, "s")).toBe(false);
    expect(num(args, "n")).toBe(3);
    expect(num(args, "badN", 7)).toBe(7);
    expect(num(null, "n", 1)).toBe(1);
  });
});

describe("callDurationMs", () => {
  it("reads duration_ms defensively from the raw record", () => {
    expect(callDurationMs({ duration_ms: 1250 })).toBe(1250);
  });

  it("returns undefined when the field has not landed (pre-change records, missing binding)", () => {
    expect(callDurationMs({})).toBeUndefined();
    expect(callDurationMs(undefined)).toBeUndefined();
    expect(callDurationMs(null)).toBeUndefined();
    expect(callDurationMs({ duration_ms: "fast" })).toBeUndefined();
    expect(callDurationMs({ duration_ms: -5 })).toBeUndefined();
  });

  it("a zero duration marks a pre-change record — no fabricated latency", () => {
    expect(callDurationMs({ duration_ms: 0 })).toBeUndefined();
  });
});
