/**
 * Tool-path extraction tests (task 8.3): the splitter finds the path tokens
 * tool outputs carry — mount-scoped workspace paths, foreign absolute paths,
 * and workspace-relative paths with an extension — while leaving URLs,
 * plain words, and score-like fragments unlinked; the mapping strips the
 * /workspace mount prefix and passes everything else through, so foreign
 * paths reach the jailed reader and are refused there.
 */
import { describe, expect, it } from "vite-plus/test";

import { splitPaths, toWorkspaceRel } from "../toolPaths";
import type { PathSegment } from "../toolPaths";

/** The path tokens of a split, in order. */
function paths(text: string): string[] {
  return splitPaths(text)
    .map((s: PathSegment) => s.path)
    .filter((p: string | null): p is string => p !== null);
}

describe("splitPaths", () => {
  it("finds mount-scoped workspace paths", () => {
    expect(paths("wrote /workspace/NOTES.md for you")).toEqual([
      "/workspace/NOTES.md",
    ]);
  });

  it("finds foreign absolute paths so they can be refused", () => {
    expect(paths("see /etc/passwd for details")).toEqual(["/etc/passwd"]);
  });

  it("finds workspace-relative paths ending in an extension", () => {
    expect(paths("transcript: .tasks/task-1.md done")).toEqual([
      ".tasks/task-1.md",
    ]);
    expect(paths("edited src/main.go today")).toEqual(["src/main.go"]);
  });

  it("finds several tokens and keeps the text between them", () => {
    const text = "/workspace/a.md and .tasks/b.md";
    const segments = splitPaths(text);

    expect(paths(text)).toEqual(["/workspace/a.md", ".tasks/b.md"]);
    expect(segments.map((s: PathSegment) => s.text).join("")).toBe(text);
  });

  it("leaves URLs, bare words, and score fragments unlinked", () => {
    expect(
      paths("open https://example.com/docs/readme.md or read the docs")
    ).toEqual([]);
    expect(paths("scored 5/5 and v1.2.3 shipped")).toEqual([]);
    expect(paths("no paths in this result")).toEqual([]);
  });

  it("leaves trailing punctuation outside the token", () => {
    expect(paths("saved to /workspace/notes.md, then verified.")).toEqual([
      "/workspace/notes.md",
    ]);
  });

  it("reconstructs the original text exactly", () => {
    const text = "diff between /workspace/a.md and .tasks/task-2.md.";

    expect(
      splitPaths(text)
        .map((s: PathSegment) => s.text)
        .join("")
    ).toBe(text);
  });
});

describe("toWorkspaceRel", () => {
  it("strips the /workspace mount prefix", () => {
    expect(toWorkspaceRel("/workspace/notes.md")).toBe("notes.md");
    expect(toWorkspaceRel("/workspace/.tasks/task-1.md")).toBe(
      ".tasks/task-1.md"
    );
  });

  it("maps the bare mount to the workspace root", () => {
    expect(toWorkspaceRel("/workspace")).toBe("");
  });

  it("passes foreign and relative paths through unchanged", () => {
    expect(toWorkspaceRel("/etc/passwd")).toBe("/etc/passwd");
    expect(toWorkspaceRel(".tasks/task-1.md")).toBe(".tasks/task-1.md");
  });
});
