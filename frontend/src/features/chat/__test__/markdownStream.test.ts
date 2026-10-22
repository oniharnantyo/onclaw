/**
 * Streaming-markdown pre-render pass tests (task 9.2, spec chat-view
 * "Streaming-safe markdown"): dangling fences get closers, unclosed inline
 * code and emphasis get their markers closed at the last prose line, and a
 * source with nothing dangling passes through byte-identical — the final
 * render of a streamed message equals a one-pass render because the
 * completed text never sees this module's edits.
 */
import { describe, expect, it } from "vite-plus/test";

import { stabilizeMarkdown } from "../lib/markdownStream";

describe("stabilizeMarkdown (task 9.2)", () => {
  it("passes a source with nothing dangling through byte-identical", () => {
    const complete = [
      "# Plan",
      "",
      "Some *emphasis*, `inline code`, and **bold**.",
      "",
      "```go",
      'fmt.Println("hi")',
      "```",
      "",
      "done.",
    ].join("\n");

    expect(stabilizeMarkdown(complete)).toBe(complete);
    expect(stabilizeMarkdown("")).toBe("");
    expect(stabilizeMarkdown("plain text only")).toBe("plain text only");
  });

  it("closes a dangling ``` fence so only the fenced text so far is code", () => {
    const partial = "before\n\n```go\nx := 1\n";

    expect(stabilizeMarkdown(partial)).toBe("before\n\n```go\nx := 1\n```");
  });

  it("closes a dangling fence directly after the opener line", () => {
    expect(stabilizeMarkdown("```ts")).toBe("```ts\n```");
  });

  it("closes a dangling ~~~ fence with the same marker", () => {
    const partial = "~~~\ncontent";

    expect(stabilizeMarkdown(partial)).toBe("~~~\ncontent\n~~~");
  });

  it("closes a long fence with an equally long closer", () => {
    expect(stabilizeMarkdown("````md\ninner ``` fence")).toBe(
      "````md\ninner ``` fence\n````"
    );
  });

  it("keeps fences closed by a bare marker while info strings stay openers", () => {
    // The second ``` line carries text, so it is content, not a closer —
    // the fence is still open at EOF and gets closed.
    const partial = "```go\n``` not a closer alone\nstill code";

    expect(stabilizeMarkdown(partial)).toBe(partial + "\n```");

    // A proper bare closer balances the pair; the trailing fence is content.
    const open2 = "```go\n```\nnot code\n```py\ncode";

    expect(stabilizeMarkdown(open2)).toBe(open2 + "\n```");
  });

  it("does not let fence content's markers leak into the prose count", () => {
    // The fence's `*`/`` ` `` markers stay literal inside the block; only the
    // prose's dangling ** is closed, and the fence gets its closer.
    const partial = "**bold prose\n\n```py\nunclosed *emphasis* and `code";

    expect(stabilizeMarkdown(partial)).toBe(
      "**bold prose**\n\n```py\nunclosed *emphasis* and `code\n```"
    );
  });

  it("closes an unclosed inline code marker at the end of its own line", () => {
    expect(stabilizeMarkdown("use `fmt to print")).toBe("use `fmt to print`");
    // The closer lands on the open run's line, never past later prose.
    expect(stabilizeMarkdown("use `fmt\n\nmore prose")).toBe(
      "use `fmt`\n\nmore prose"
    );
  });

  it("matches only equal-length backtick runs", () => {
    // A ``` run inside prose cannot close a ` span (CommonMark).
    const partial = "a `b and ``` c";

    expect(stabilizeMarkdown(partial)).toBe(partial + "`");
  });

  it("closes unclosed bold and italic markers", () => {
    expect(stabilizeMarkdown("this is **bold")).toBe("this is **bold**");
    expect(stabilizeMarkdown("this is *ital")).toBe("this is *ital*");
    // Both open at once: both closers, bold first.
    expect(stabilizeMarkdown("**b and *i")).toBe("**b and *i***");
  });

  it("counts *** runs as balanced bold+italic pairs", () => {
    expect(stabilizeMarkdown("***x*** stays")).toBe("***x*** stays");
  });

  it("never counts emphasis inside inline code spans", () => {
    // The `**` pair inside the code span is literal; nothing is dangling.
    expect(stabilizeMarkdown("a `**b**` c")).toBe("a `**b**` c");
    // ...and with the span still open, the `**` sits inside the span the
    // pending closer will form, so only the backtick closes.
    expect(stabilizeMarkdown("a `b **c")).toBe("a `b **c`");
  });

  it("appends the inline closer before a later fence, never after it", () => {
    // The backtick is dangling in the prose ABOVE the balanced fence; the
    // closer must land on the prose line, not past the block.
    const partial = "some `text\n\n```go\nx := 1\n```\n\nafter";

    expect(stabilizeMarkdown(partial)).toBe(
      "some `text`\n\n```go\nx := 1\n```\n\nafter"
    );
  });

  it("keeps spaced asterisks untouched", () => {
    expect(stabilizeMarkdown("2 * 3 * 4 = 6")).toBe("2 * 3 * 4 = 6");
  });

  it("is idempotent on its own output", () => {
    const partial = "before\n\n```go\nx := 1";

    const once = stabilizeMarkdown(partial);

    expect(stabilizeMarkdown(once)).toBe(once);
  });
});
