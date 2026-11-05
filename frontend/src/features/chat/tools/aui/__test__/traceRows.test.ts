/**
 * Trace-row chip tests: the chip names the call's target from its JSON
 * args, and a mapped task record's plain-text payload degrades to the chip
 * instead of an empty box.
 */
import { describe, expect, it } from "vite-plus/test";

import { toolChip, toolIcon, toolVerb } from "../traceRows";

describe("toolChip", () => {
  it("reads the target off JSON args", () => {
    expect(
      toolChip("agent", JSON.stringify({ description: "scan the repo" }))
    ).toBe("scan the repo");
  });

  it("falls back to the plain-text payload of a mapped task record", () => {
    expect(toolChip("task", "Research self-improving agent harnesses")).toBe(
      "Research self-improving agent harnesses"
    );
  });

  it("caps a long plain-text payload at the chip cut", () => {
    expect(toolChip("task", "x".repeat(100))).toBe(`${"x".repeat(77)}...`);
  });

  it("keeps the empty chip for unparsable non-task payloads", () => {
    expect(toolChip("execute", "not json")).toBe("");
  });

  it("aliases shell onto the execute chip", () => {
    expect(toolChip("shell", '{"command":"go test ./..."}')).toBe("go");
  });

  it("maps shell onto the same verb and icon as execute", () => {
    expect(toolVerb("shell")).toBe(toolVerb("execute"));
    expect(toolIcon("shell")).toBe(toolIcon("execute"));
  });
});

describe("browser-use trace rows (rebrand-browser-use-system-mcp 6.2)", () => {
  it("maps the mcp__browser-use__* names onto the Browse verb and globe glyph", () => {
    for (const name of [
      "mcp__browser-use__navigate",
      "mcp__browser-use__click",
      "mcp__browser-use__wait",
    ]) {
      expect(toolVerb(name)).toBe("Browse");
      expect(toolIcon(name)).toBe(toolIcon("web_search"));
    }
  });

  it("chips the page address off navigate args", () => {
    expect(
      toolChip("mcp__browser-use__navigate", '{"url":"https://example.com"}')
    ).toBe("https://example.com");
  });

  it("chips the key/text/ref scalars when no url is present", () => {
    expect(toolChip("mcp__browser-use__press_key", '{"key":"Enter"}')).toBe(
      "Enter"
    );
    expect(
      toolChip("mcp__browser-use__type", '{"text":"hello","ref":"e3"}')
    ).toBe("hello");
    expect(toolChip("mcp__browser-use__click", '{"ref":"e3"}')).toBe("e3");
  });
});
