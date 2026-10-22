/**
 * Unit tests for the transcript reducer (task 9.2): every session event kind
 * folds into the right entry shape, deltas accumulate live-only, durable
 * events reconcile against streaming ones, and event-id dedupe keeps the
 * replay from duplicating rows the live stream already applied.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, type Entry } from "../entries";

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

describe("transcript reducer", () => {
  it("folds a user message into a user entry", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "hello", turn_id: "t1", id: "e1" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "user",
      text: "hello",
      turnID: "t1",
    });
  });

  it("accumulates text deltas into one streaming assistant entry", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "He", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "llo", turn_id: "t1" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello",
      streaming: true,
    });
  });

  it("accumulates reasoning deltas into a reasoning entry", () => {
    const entries = fold(
      ev(Kind.KindReasoningDelta, { text: "think", turn_id: "t1" }),
      ev(Kind.KindReasoningDelta, { text: "ing", turn_id: "t1" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "reasoning",
      text: "thinking",
      streaming: true,
    });
  });

  it("a delta after a tool card opens a new assistant entry (order preserved)", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "checking", turn_id: "t1" }),
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: { call_id: "c1", name: "search", arguments: "{}" },
      }),
      ev(Kind.KindTextDelta, { text: "done", turn_id: "t1" })
    );

    expect(entries.map((e: Entry) => e.kind)).toEqual([
      "assistant",
      "tool",
      "assistant",
    ]);
    expect(entries[2]).toMatchObject({ text: "done", streaming: true });
  });

  it("assistant_message finalizes the streaming entry with the durable text", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "Hel", turn_id: "t1" }),
      ev(Kind.KindAssistantMessage, {
        text: "Hello world",
        turn_id: "t1",
        id: "e2",
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello world",
      streaming: false,
    });
  });

  it("assistant_message without a prior streaming entry appends", () => {
    const entries = fold(
      ev(Kind.KindAssistantMessage, { text: "hi", id: "e1" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "hi",
      streaming: false,
    });
  });

  it("reasoning (durable) finalizes the streaming reasoning entry", () => {
    const entries = fold(
      ev(Kind.KindReasoningDelta, { text: "thi", turn_id: "t1" }),
      ev(Kind.KindReasoning, { text: "thinking", turn_id: "t1", id: "e2" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "reasoning",
      text: "thinking",
      streaming: false,
    });
  });

  it("tool activity mints one card per call id; a repeat mutates it", () => {
    const entries = fold(
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: { call_id: "c1", name: "search", arguments: "{}" },
      }),
      ev(Kind.KindToolActivity, {
        turn_id: "t1",
        tool: {
          call_id: "c1",
          name: "search",
          arguments: "{}",
          result: "found it",
        },
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "tool",
      call: { call_id: "c1", result: "found it" },
    });
  });

  it("task activity mints one card per task id; the terminal record mutates it", () => {
    const entries = fold(
      ev(Kind.KindTaskActivity, {
        turn_id: "t1",
        task: {
          task_id: "task-1",
          description: "Research",
          status: ToolStatus.ToolStatusRunning,
          background: true,
        },
      }),
      ev(Kind.KindTaskActivity, {
        turn_id: "t1",
        task: {
          task_id: "task-1",
          description: "Research",
          status: ToolStatus.ToolStatusCompleted,
          result: "done",
          transcript_path: ".tasks/task-1.md",
          background: true,
        },
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "task",
      task: { task_id: "task-1", status: "completed", result: "done" },
    });
  });

  it("task activity without a task payload is ignored", () => {
    const entries = fold(ev(Kind.KindTaskActivity, { turn_id: "t1" }));

    expect(entries).toHaveLength(0);
  });

  it("compaction appends a marker entry", () => {
    const entries = fold(
      ev(Kind.KindCompaction, {
        compaction: {
          checkpoint_id: "cp1",
          tokens_before: 150000,
          tokens_after: 9000,
        },
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].kind).toBe("compaction");
  });

  it("turn_completed finalizes streaming entries and drops empty ones", () => {
    const entries = fold(
      ev(Kind.KindReasoningDelta, { text: "hmm", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "", turn_id: "t1" }),
      ev(Kind.KindTurnCompleted, {
        turn_id: "t1",
        id: "e3",
        usage: { input_tokens: 1, output_tokens: 1, total_tokens: 2 },
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "reasoning",
      text: "hmm",
      streaming: false,
    });
  });

  it("turn_error finalizes and appends an inline error entry", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "partial", turn_id: "t1" }),
      ev(Kind.KindTurnError, {
        turn_id: "t1",
        id: "e2",
        error: "provider exploded",
      })
    );

    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "partial",
      streaming: false,
    });
    expect(entries[1]).toMatchObject({
      kind: "error",
      message: "provider exploded",
      turnID: "t1",
    });
  });

  it("turn_cancelled finalizes the streaming entry keeping its partial text (task 3.3)", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "Hel", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "lo", turn_id: "t1" }),
      ev(Kind.KindTurnCancelled, {
        turn_id: "t1",
        id: "e2",
        text: "Hello (partial)",
      })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello",
      streaming: false,
      turnID: "t1",
    });
  });

  it("turn_cancelled on replay mints the finalized entry from the payload partial", () => {
    // Deltas are live-only; a replay sees the persisted terminal alone.
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "hi", turn_id: "t1", id: "e1" }),
      ev(Kind.KindTurnCancelled, {
        turn_id: "t1",
        id: "e2",
        text: "partial answ",
      })
    );

    expect(entries).toHaveLength(2);
    expect(entries[1]).toMatchObject({
      kind: "assistant",
      text: "partial answ",
      streaming: false,
      turnID: "t1",
    });
  });

  it("turn_cancelled finalizes a streaming reasoning entry and drops empty ones", () => {
    const entries = fold(
      ev(Kind.KindReasoningDelta, { text: "thinking", turn_id: "t1" }),
      ev(Kind.KindTextDelta, { text: "", turn_id: "t1" }),
      ev(Kind.KindTurnCancelled, { turn_id: "t1", id: "e2" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "reasoning",
      text: "thinking",
      streaming: false,
    });
  });

  it("turn_cancelled without partial output or streaming entries changes nothing", () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "hi", turn_id: "t1", id: "e1" }),
      ev(Kind.KindTurnCancelled, { turn_id: "t1", id: "e2" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0].kind).toBe("user");
  });

  it("skips a persisted event already applied (store-level dedupe contract)", () => {
    // The store dedupes by event id before calling applyEvent; folding the
    // same event twice through that gate yields one entry.
    const e = ev(Kind.KindUserMessage, { text: "hello", id: "e1" });
    const seen = new Set<string>();

    const entries = [e, e].reduce<Entry[]>(
      (acc: Entry[], ev2: SessionEvent) => {
        if (ev2.id) {
          if (seen.has(ev2.id)) return acc;
          seen.add(ev2.id);
        }

        return applyEvent(acc, ev2);
      },
      []
    );

    expect(entries).toHaveLength(1);
  });

  it("applies live-only deltas without ids repeatedly", () => {
    const entries = fold(
      ev(Kind.KindTextDelta, { text: "a" }),
      ev(Kind.KindTextDelta, { text: "b" })
    );

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ text: "ab", streaming: true });
  });
});
