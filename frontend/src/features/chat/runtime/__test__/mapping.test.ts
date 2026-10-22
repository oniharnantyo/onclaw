/**
 * Entry → message mapping tests (task 2.1, design D2): every mapping rule —
 * user messages, turn grouping (turnID and adjacency fallback), each part
 * type, compaction as its own message, error status, createdAt passthrough,
 * streaming status, the approval part lifecycle, attachments — plus the
 * fixture-replay assertion: entries assembled by folding a recorded event
 * sequence map identically to the same entries assembled directly.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import {
  applyEvent,
  asApproval,
  asEntry,
  asRichUser,
  type Entry,
} from "../../entries";
import { entriesToMessages } from "../mapping";

function ev(
  kind: SessionEvent["kind"],
  extra: Partial<SessionEvent> = {}
): SessionEvent {
  return { kind, occurred_at: "2026-01-01T00:00:00Z", ...extra };
}

function fold(...events: SessionEvent[]): Entry[] {
  return events.reduce<Entry[]>(
    (entries: Entry[], e: SessionEvent) => applyEvent(entries, e),
    []
  );
}

/** Re-keys one entry, re-forming its exact kind (the mapping must depend on
 * entry content, not on how the reducer minted ids). The widened kinds ride
 * the string-comparison seam the reducer itself uses. */
function relabeled(entry: Entry, id: string): Entry {
  const kind: string = entry.kind;

  // SAFETY: only applyEvent mints kind "approval" (ApprovalEntry); the
  // widened read keeps the literal-union comparison legal.
  if (kind === "approval") return asEntry({ ...asApproval(entry), id });

  // SAFETY: as above — user entries minted by applyEvent carry the
  // RichUserEntry fields alongside the kind.
  if (kind === "user") return asEntry({ ...asRichUser(entry), id });

  return { ...entry, id };
}

describe("entry → message mapping (task 2.1, design D2)", () => {
  it("maps a user entry to a user message carrying its attachments", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, {
        text: "see this",
        turn_id: "t1",
        id: "e1",
        attachments: [
          { key: "k-img", name: "shot.png", mime: "image/png", size: 10 },
          { key: "k-doc", name: "spec.pdf", mime: "application/pdf", size: 20 },
        ],
      })
    );

    const [user] = entriesToMessages(entries);

    expect(user).toEqual({
      role: "user",
      id: entries[0].id,
      createdAt: new Date("2026-01-01T00:00:00Z"),
      content: [{ type: "text", id: entries[0].id, text: "see this" }],
      attachments: [
        {
          id: "k-img",
          type: "image",
          name: "shot.png",
          contentType: "image/png",
          content: [],
          status: { type: "complete" },
        },
        {
          id: "k-doc",
          type: "document",
          name: "spec.pdf",
          contentType: "application/pdf",
          content: [],
          status: { type: "complete" },
        },
      ],
    });
  });

  it("groups a turn's entries into one assistant message in order", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "run it", turn_id: "t1", id: "e1" }),
      ev(Kind.KindReasoningDelta, { text: "think", turn_id: "t1" }),
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c1",
          name: "execute",
          arguments: '{"command":"ls"}',
          status: ToolStatus.ToolStatusCompleted,
          result: "a.txt",
          is_error: false,
        },
      }),
      ev(Kind.KindTextDelta, { text: "Done — ", turn_id: "t1" }),
      ev(Kind.KindAssistantMessage, {
        text: "Done — a.txt",
        turn_id: "t1",
        id: "e2",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" })
    );

    const [user, turn] = entriesToMessages(entries);

    expect(user).toMatchObject({ role: "user", content: [{ text: "run it" }] });
    expect(turn).toMatchObject({
      role: "assistant",
      status: { type: "complete", reason: "stop" },
    });
    expect(turn?.content).toEqual([
      { type: "reasoning", id: entries[1].id, text: "think" },
      {
        type: "tool-call",
        toolCallId: entries[2].id,
        toolName: "execute",
        args: { command: "ls" },
        argsText: '{"command":"ls"}',
        isError: false,
        result: "a.txt",
      },
      { type: "text", id: entries[3].id, text: "Done — a.txt" },
    ]);
  });

  it("groups legacy turn-less entries by adjacency, never into a stamped turn", () => {
    const entries = fold(
      ev(Kind.KindAssistantMessage, {
        text: "stamped one",
        turn_id: "t1",
        id: "e1",
      }),
      ev(Kind.KindAssistantMessage, { text: "legacy text", id: "e2" }),
      ev(Kind.KindToolActivity, {
        id: "e3",
        tool: { call_id: "c9", name: "grep", arguments: "{}" },
      }),
      ev(Kind.KindAssistantMessage, {
        text: "stamped two",
        turn_id: "t2",
        id: "e4",
      })
    );

    const messages = entriesToMessages(entries);

    expect(messages).toHaveLength(3);
    expect(messages[1]?.content).toEqual([
      { type: "text", id: entries[1].id, text: "legacy text" },
      {
        type: "tool-call",
        toolCallId: entries[2].id,
        toolName: "grep",
        args: {},
        argsText: "{}",
        isError: false,
      },
    ]);
  });

  it("makes a compaction entry its own data-compaction message between turns", () => {
    const entries = fold(
      ev(Kind.KindAssistantMessage, {
        text: "before",
        turn_id: "t1",
        id: "e1",
      }),
      ev(Kind.KindCompaction, {
        id: "e2",
        compaction: {
          checkpoint_id: "cp1",
          tokens_before: 100,
          tokens_after: 40,
        },
      }),
      ev(Kind.KindAssistantMessage, { text: "after", turn_id: "t2", id: "e3" })
    );

    const messages = entriesToMessages(entries);

    expect(messages).toHaveLength(3);
    expect(messages[1]).toEqual({
      role: "assistant",
      id: entries[1].id,
      createdAt: new Date("2026-01-01T00:00:00Z"),
      content: [
        {
          type: "data-compaction",
          id: entries[1].id,
          data: { checkpoint_id: "cp1", tokens_before: 100, tokens_after: 40 },
        },
      ],
      status: { type: "complete", reason: "stop" },
    });
  });

  it("marks a turn with an error entry incomplete beside its data-error part", () => {
    const entries = fold(
      ev(Kind.KindAssistantMessage, {
        text: "partial",
        turn_id: "t1",
        id: "e1",
      }),
      ev(Kind.KindTurnError, { turn_id: "t1", error: "boom", id: "e2" })
    );

    const [turn] = entriesToMessages(entries);

    expect(turn).toMatchObject({
      role: "assistant",
      status: { type: "incomplete", reason: "error" },
    });
    expect(turn?.content).toEqual([
      { type: "text", id: entries[0].id, text: "partial" },
      { type: "data-error", id: entries[1].id, data: { message: "boom" } },
    ]);
  });

  it("passes each entry's timestamp through as the message createdAt", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, {
        text: "hi",
        turn_id: "t1",
        id: "e1",
        occurred_at: "2026-03-04T05:06:07Z",
      }),
      ev(Kind.KindAssistantMessage, {
        text: "hello",
        turn_id: "t1",
        id: "e2",
        occurred_at: "2026-03-04T05:06:09Z",
      })
    );

    const [user, turn] = entriesToMessages(entries);

    expect(user?.createdAt).toEqual(new Date("2026-03-04T05:06:07Z"));
    expect(turn?.createdAt).toEqual(new Date("2026-03-04T05:06:09Z"));
  });

  it("maps a streaming turn to a running message with running parts", () => {
    const streaming = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1", id: "e1" }),
      ev(Kind.KindReasoningDelta, { text: "hmm", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "so", turn_id: "t1" })
    );

    const [, turn] = entriesToMessages(streaming);

    expect(turn).toMatchObject({
      role: "assistant",
      status: { type: "running" },
    });
    expect(turn?.content).toEqual([
      {
        type: "reasoning",
        id: streaming[1].id,
        text: "hmm",
        status: { type: "running" },
      },
      {
        type: "text",
        id: streaming[2].id,
        text: "so",
        status: { type: "running" },
      },
    ]);

    // The durable replay form of the same turn settles everything.
    const done = fold(
      ev(Kind.KindUserMessage, { text: "go", turn_id: "t1", id: "e1" }),
      ev(Kind.KindReasoning, { text: "hmm", turn_id: "t1", id: "e2" }),
      ev(Kind.KindAssistantMessage, { text: "so", turn_id: "t1", id: "e3" }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" })
    );

    const [, doneTurn] = entriesToMessages(done);

    expect(doneTurn).toMatchObject({
      status: { type: "complete", reason: "stop" },
    });
    expect(doneTurn?.content).toEqual([
      { type: "reasoning", id: done[1].id, text: "hmm" },
      { type: "text", id: done[2].id, text: "so" },
    ]);
  });

  it("maps a live tool call to a result-less part until the record lands", () => {
    const entries = fold(
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c1",
          name: "execute",
          arguments: "{}",
          status: ToolStatus.ToolStatusRunning,
        },
      })
    );

    const [turn] = entriesToMessages(entries);

    expect(turn?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: entries[0].id,
        toolName: "execute",
        args: {},
        argsText: "{}",
        isError: false,
      },
    ]);
  });

  it("maps a delegated task to the registry's task tool-call part", () => {
    const running = fold(
      ev(Kind.KindTaskActivity, {
        turn_id: "t1",
        task: {
          task_id: "task-1",
          description: "scan",
          status: ToolStatus.ToolStatusRunning,
        },
      })
    );

    const [runningTurn] = entriesToMessages(running);

    expect(runningTurn?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: running[0].id,
        toolName: "task",
        args: { task_id: "task-1", description: "scan" },
        argsText: "scan",
        isError: false,
      },
    ]);

    const done = fold(
      ev(Kind.KindTaskActivity, {
        turn_id: "t1",
        task: {
          task_id: "task-1",
          description: "scan",
          status: ToolStatus.ToolStatusFailed,
          result: "no match",
          transcript_path: ".tasks/task-1.md",
        },
      })
    );

    const [doneTurn] = entriesToMessages(done);

    expect(doneTurn?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: done[0].id,
        toolName: "task",
        args: {
          task_id: "task-1",
          description: "scan",
          transcript_path: ".tasks/task-1.md",
        },
        argsText: "scan",
        isError: true,
        result: "no match",
      },
    ]);
  });

  it("maps a pending approval onto the turn's tool-call part awaiting action", () => {
    const entries = fold(
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c1",
          name: "execute",
          arguments: "{}",
          status: ToolStatus.ToolStatusCompleted,
          result: "ok",
        },
      }),
      ev(Kind.KindApprovalRequired, {
        turn_id: "t1",
        approval: { interrupt_id: "int-1", command: "rm -rf /tmp/x" },
      })
    );

    const [turn] = entriesToMessages(entries);

    expect(turn).toMatchObject({
      role: "assistant",
      status: { type: "requires-action", reason: "interrupt" },
    });
    expect(turn?.content[1]).toEqual({
      type: "tool-call",
      toolCallId: entries[1].id,
      toolName: "execute",
      args: { command: "rm -rf /tmp/x" },
      argsText: "rm -rf /tmp/x",
      approval: { id: "int-1" },
    });
  });

  it("records an approval decision on the same part when resolved", () => {
    const resolved = (outcome: string): Entry[] =>
      fold(
        ev(Kind.KindApprovalRequired, {
          turn_id: "t1",
          approval: { interrupt_id: "int-1", command: "make deploy" },
        }),
        ev(Kind.KindApprovalResolved, {
          turn_id: "t1",
          approval: { interrupt_id: "int-1", outcome },
        })
      );

    const approvedEntries = resolved("approved");
    const approved = entriesToMessages(approvedEntries);

    expect(approved[0]).toMatchObject({
      status: { type: "complete", reason: "stop" },
    });
    expect(approved[0]?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: approvedEntries[0].id,
        toolName: "execute",
        args: { command: "make deploy" },
        argsText: "make deploy",
        approval: { id: "int-1", approved: true },
      },
    ]);

    const denied = entriesToMessages(resolved("denied"));

    expect(denied[0]?.content[0]).toMatchObject({
      approval: { id: "int-1", approved: false },
    });

    const cancelled = entriesToMessages(resolved("cancelled"));

    expect(cancelled[0]?.content[0]).toMatchObject({
      approval: { id: "int-1", resolution: "cancelled" },
    });
  });

  it("maps a stamped duration onto the part's timing and a legacy record onto none", () => {
    // SAFETY: duration_ms is omitted from the wire type; the literal pins
    // the stamped-record shape the binding delivers.
    // A terminal record carrying duration_ms: end stamped at the record's
    // arrival time, start derived back from the duration.
    const stamped = fold(
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        occurred_at: "2026-01-01T00:00:02Z",
        tool: {
          call_id: "c1",
          name: "execute",
          arguments: "{}",
          status: ToolStatus.ToolStatusCompleted,
          result: "ok",
          duration_ms: 1500,
        } as SessionEvent["tool"],
      })
    );

    const [stampedTurn] = entriesToMessages(stamped);

    expect(stampedTurn?.content[0]).toMatchObject({
      type: "tool-call",
      timing: {
        startedAt: Date.parse("2026-01-01T00:00:00.500Z"),
        completedAt: Date.parse("2026-01-01T00:00:02Z"),
      },
    });

    // A record persisted before durations were recorded: no timing, so the
    // card shows no latency rather than a fabricated one.
    const legacy = fold(
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c2",
          name: "execute",
          arguments: "{}",
          result: "ok",
        },
      })
    );

    const [legacyTurn] = entriesToMessages(legacy);

    expect(legacyTurn?.content[0]).toMatchObject({ type: "tool-call" });
    expect(legacyTurn?.content[0]).not.toHaveProperty("timing");
  });

  it("maps a recorded event fixture and directly assembled entries identically", () => {
    // A recorded page: user prompt, a tool round, the answer, then a
    // regeneration variant of the same prompt.
    const recorded: SessionEvent[] = [
      ev(Kind.KindUserMessage, { text: "explain", turn_id: "t1", id: "e1" }),
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c1",
          name: "read_file",
          arguments: '{"path":"a"}',
          status: ToolStatus.ToolStatusCompleted,
          result: "...",
        },
      }),
      ev(Kind.KindAssistantMessage, {
        text: "it does x",
        turn_id: "t1",
        id: "e2",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1", id: "e3" }),
      ev(Kind.KindUserMessage, { turn_id: "t2", variant_of: "t1", id: "e4" }),
      ev(Kind.KindAssistantMessage, {
        text: "shorter",
        turn_id: "t2",
        variant_of: "t1",
        id: "e5",
      }),
      ev(Kind.KindTurnCompleted, { turn_id: "t2", variant_of: "t1", id: "e6" }),
    ];

    const replayed = fold(...recorded).map((entry: Entry, i: number) =>
      relabeled(entry, `x${i}`)
    );

    // The same entries assembled directly, labels x0..x3 in fold order: the
    // variant marker widens the anchor with its slot and stamps the reply.
    const variantTag = { variantOf: "t1", variantIndex: 1 };

    const direct: Entry[] = [
      asEntry({
        kind: "user",
        id: "x0",
        turnID: "t1",
        text: "explain",
        at: "2026-01-01T00:00:00Z",
        variantTurns: ["t1", "t2"],
        activeVariant: 1,
      }),
      {
        kind: "tool",
        id: "x1",
        turnID: "t1",
        call: {
          call_id: "c1",
          name: "read_file",
          arguments: '{"path":"a"}',
          status: ToolStatus.ToolStatusCompleted,
          result: "...",
        },
        at: "2026-01-01T00:00:00Z",
      },
      {
        kind: "assistant",
        id: "x2",
        turnID: "t1",
        text: "it does x",
        streaming: false,
        at: "2026-01-01T00:00:00Z",
      },
      {
        kind: "assistant",
        id: "x3",
        turnID: "t2",
        text: "shorter",
        streaming: false,
        at: "2026-01-01T00:00:00Z",
        ...variantTag,
      },
    ];

    expect(entriesToMessages(replayed)).toEqual(entriesToMessages(direct));
  });
});
