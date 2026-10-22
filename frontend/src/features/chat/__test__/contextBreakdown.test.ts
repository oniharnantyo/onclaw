/**
 * Context breakdown estimator tests (design D7, task 6.1): every rule at
 * face value — 4 chars/token, 8 per message, 120 per tool, 40 per skill
 * header, 1100 per image, text attachments by size, post-compaction
 * entries only, the server remainder floored at zero, and estimates never
 * scaled to the reported total.
 */
import { describe, expect, it } from "vite-plus/test";

import type { Attachment as SessionAttachment } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import {
  computeContextBreakdown,
  estimateInstructions,
  postCompactionEntries,
  TOKENS_PER_IMAGE,
  TOKENS_PER_MESSAGE,
  TOKENS_PER_SKILL,
  TOKENS_PER_TOOL,
  type BreakdownInput,
  type ContextSegment,
  type PromptDocContent,
} from "../lib/contextBreakdown";
import type { RichUserEntry } from "../entries";
import type {
  AssistantEntry,
  CompactionEntry,
  Entry,
  ReasoningEntry,
  ToolEntry,
  UserEntry,
} from "../types";

function user(text: string, attachments?: SessionAttachment[]): Entry {
  const entry: UserEntry = { kind: "user", id: "u", text, at: "" };

  if (!attachments) return entry;

  const rich: RichUserEntry = { ...entry, attachments };

  return rich;
}

function assistant(text: string): AssistantEntry {
  return { kind: "assistant", id: "a", text, streaming: false, at: "" };
}

function reasoning(text: string): ReasoningEntry {
  return { kind: "reasoning", id: "r", text, streaming: false, at: "" };
}

function toolCall(): ToolEntry {
  return {
    kind: "tool",
    id: "t",
    call: { call_id: "call-1", name: "read_file", arguments: "{}" },
    at: "",
  };
}

function compaction(): CompactionEntry {
  return {
    kind: "compaction",
    id: "c",
    compaction: {
      checkpoint_id: "cp-1",
      tokens_before: 150_000,
      tokens_after: 9_000,
    },
    at: "",
  };
}

function image(name: string): SessionAttachment {
  return { key: `k-${name}`, name, mime: "image/png", size: 40_000 };
}

function textFile(size: number): SessionAttachment {
  return { key: "k-txt", name: "notes.txt", mime: "text/plain", size };
}

function input(overrides: Partial<BreakdownInput>): BreakdownInput {
  return {
    used: 0,
    limit: 200_000,
    promptDocs: null,
    enabledTools: 0,
    enabledSkills: 0,
    entries: [],
    ...overrides,
  };
}

function segment(
  tokensOf: BreakdownInput,
  key: "instructions" | "tools" | "files" | "conversation" | "server"
): number {
  const breakdown = computeContextBreakdown(tokensOf);

  const hit = breakdown.segments.find(
    (s: ContextSegment): boolean => s.key === key
  );

  expect(hit).toBeTruthy();

  return hit!.tokens;
}

describe("contextBreakdown conversation", () => {
  it("counts 4 chars per token plus 8 per message", () => {
    // 40 chars → 10 tokens + the flat 8.
    const tokens = segment(
      input({ entries: [user("x".repeat(40))] }),
      "conversation"
    );

    expect(tokens).toBe(TOKENS_PER_MESSAGE + 10);
  });

  it("rounds a partial token up for short messages", () => {
    // 2 chars → 1 token + the flat 8.
    expect(segment(input({ entries: [user("hi")] }), "conversation")).toBe(9);
  });

  it("counts user, assistant, and reasoning messages but not tool cards", () => {
    const entries = [
      user("x".repeat(40)),
      assistant("y".repeat(40)),
      reasoning("z".repeat(40)),
      toolCall(),
    ];

    // Three messages at 18 each; the tool entry's arguments stay unweighed.
    expect(segment(input({ entries }), "conversation")).toBe(54);
  });
});

describe("contextBreakdown tools & skills", () => {
  it("counts 120 per enabled tool and 40 per skill header", () => {
    const tokens = segment(
      input({ enabledTools: 2, enabledSkills: 3 }),
      "tools"
    );

    expect(tokens).toBe(2 * TOKENS_PER_TOOL + 3 * TOKENS_PER_SKILL);
  });

  it("counts zero without tools or skills", () => {
    expect(segment(input({}), "tools")).toBe(0);
  });
});

describe("contextBreakdown files", () => {
  it("counts 1100 per image attachment", () => {
    const entries = [user("", [image("a.png"), image("b.png")])];

    expect(segment(input({ entries }), "files")).toBe(2 * TOKENS_PER_IMAGE);
  });

  it("sizes text attachments at 4 chars per token", () => {
    const entries = [user("", [textFile(400)])];

    expect(segment(input({ entries }), "files")).toBe(100);
  });

  it("mixes images and text attachments", () => {
    const entries = [user("", [image("a.png"), textFile(80)])];

    expect(segment(input({ entries }), "files")).toBe(TOKENS_PER_IMAGE + 20);
  });
});

describe("contextBreakdown instructions", () => {
  it("sizes the existing prompt documents at 4 chars per token", () => {
    const docs: PromptDocContent = {
      Identity: "i".repeat(100),
      IdentityExists: true,
      Soul: "s".repeat(56),
      SoulExists: true,
      Bootstrap: "ignored",
      BootstrapExists: false,
    };

    expect(estimateInstructions(docs)).toBe(39);
  });

  it("estimates zero while the documents are unread (null)", () => {
    expect(estimateInstructions(null)).toBe(0);
  });
});

describe("contextBreakdown compaction", () => {
  it("counts only entries after the last compaction marker", () => {
    const entries = [
      user("x".repeat(40)),
      compaction(),
      assistant("y".repeat(40)),
    ];

    const payload = input({ entries, used: 100_000 });

    // The pre-compaction message is excluded; the post-compaction one counts.
    expect(segment(payload, "conversation")).toBe(TOKENS_PER_MESSAGE + 10);
    expect(segment(payload, "files")).toBe(0);
  });

  it("excludes attachments on pre-compaction messages", () => {
    const entries = [
      user("", [image("early.png")]),
      compaction(),
      user("", [image("late.png")]),
    ];

    expect(segment(input({ entries }), "files")).toBe(TOKENS_PER_IMAGE);
  });

  it("counts everything when no compaction marker exists", () => {
    const entries = [user("x".repeat(40)), assistant("y".repeat(40))];

    expect(segment(input({ entries }), "conversation")).toBe(36);
  });

  it("postCompactionEntries slices after the last marker", () => {
    const a = user("a");
    const b = user("b");
    const marker = compaction();
    const c = user("c");
    const later = compaction();
    const d = user("d");

    expect(postCompactionEntries([a, b, marker, c, later, d])).toEqual([d]);
    expect(postCompactionEntries([a, b])).toEqual([a, b]);
  });
});

describe("contextBreakdown totals", () => {
  it("computes the server remainder as used minus the estimate sum", () => {
    const payload = input({
      used: 10_000,
      promptDocs: {
        Identity: "i".repeat(100),
        IdentityExists: true,
        Soul: "",
        SoulExists: false,
        Bootstrap: "",
        BootstrapExists: false,
      },
      enabledTools: 2,
      entries: [user("x".repeat(40))],
    });

    // 25 instructions + 240 tools + 18 conversation = 283 → 10_000 − 283.
    expect(segment(payload, "server")).toBe(10_000 - 283);
  });

  it("floors the server remainder at zero when estimates exceed used", () => {
    const payload = input({
      used: 50,
      enabledTools: 10,
      enabledSkills: 10,
    });

    expect(segment(payload, "server")).toBe(0);
  });

  it("never scales the estimates toward the reported total", () => {
    const payload = input({
      used: 500,
      promptDocs: {
        Identity: "i".repeat(40),
        IdentityExists: true,
        Soul: "",
        SoulExists: false,
        Bootstrap: "",
        BootstrapExists: false,
      },
      enabledTools: 1,
      entries: [user("x".repeat(40))],
    });

    const breakdown = computeContextBreakdown(payload);

    const estimates = breakdown.segments.filter(
      (s: ContextSegment): boolean => s.estimated
    );

    const sum = estimates.reduce(
      (acc: number, s: ContextSegment): number => acc + s.tokens,
      0
    );

    // Verbatim face values: 10 instructions + 120 tools + 18 conversation.
    expect(sum).toBe(148);
    expect(sum).not.toBe(breakdown.used);
    // The displayed total is the real reported used, untouched.
    expect(breakdown.used).toBe(500);
  });

  it("computes headroom as limit minus used", () => {
    expect(computeContextBreakdown(input({ used: 10_000 })).headroom).toBe(
      190_000
    );
  });

  it("carries the estimated flag on client segments only", () => {
    const breakdown = computeContextBreakdown(input({}));

    expect(
      breakdown.segments
        .filter((s: ContextSegment): boolean => s.estimated)
        .map((s: ContextSegment): string => s.key)
    ).toEqual(["instructions", "tools", "files", "conversation"]);
    expect(breakdown.segments[4].key).toBe("server");
    expect(breakdown.segments[4].estimated).toBe(false);
  });
});
