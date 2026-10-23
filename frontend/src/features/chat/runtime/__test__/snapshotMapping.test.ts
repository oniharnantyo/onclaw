/**
 * Catch-up snapshot reduction and mapping tests (fix-session-switch-liveness task 4.3):
 * Verifies that synthetic catch-up snapshot events emitted at subscribe time fold
 * through the transcript reducer (`applyEvent`) and derive assistant-ui messages
 * (`entriesToMessages`) seamlessly without duplicate rows or artifacts.
 *
 * Requirements tested:
 * 1. Mid-stream catch-up: snap-<turnid>-partial creates a streaming assistant entry.
 * 2. Continuation: subsequent live KindTextDelta appends to the streaming entry.
 * 3. Durable message arrival: durable KindAssistantMessage finalizes without duplicating content.
 * 4. Running tool catch-up: snap-<turnid>-tool-<callid> produces an active tool activity row.
 * 5. Tool completion: terminal KindToolActivity resolves the existing row by call_id.
 * 6. No snapshot events in durable transcript: reloading/replaying durable events contains no synthetic entries.
 */
import { describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
  type Event as SessionEvent,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, type Entry } from "../../entries";
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

describe("catch-up snapshot reduction & mapping", () => {
  it("mid-stream catch-up: snap-<turnid>-partial creates a streaming assistant entry", () => {
    const snapPartial = ev(Kind.KindTextDelta, {
      id: "snap-turn-1-partial",
      turn_id: "turn-1",
      text: "Hello ",
    });

    const entries = fold(snapPartial);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      turnID: "turn-1",
      text: "Hello ",
      streaming: true,
    });

    const [message] = entriesToMessages(entries);

    expect(message).toMatchObject({
      role: "assistant",
      id: "turn-1",
      status: { type: "running" },
    });
    expect(message?.content).toEqual([
      {
        type: "text",
        id: entries[0].id,
        text: "Hello ",
        status: { type: "running" },
      },
    ]);
  });

  it("continuation: subsequent live KindTextDelta appends to the streaming entry", () => {
    const snapPartial = ev(Kind.KindTextDelta, {
      id: "snap-turn-1-partial",
      turn_id: "turn-1",
      text: "Hello ",
    });

    const liveDelta = ev(Kind.KindTextDelta, {
      turn_id: "turn-1",
      text: "world!",
    });

    const entries = fold(snapPartial, liveDelta);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      turnID: "turn-1",
      text: "Hello world!",
      streaming: true,
    });

    const [message] = entriesToMessages(entries);

    expect(message).toMatchObject({
      role: "assistant",
      id: "turn-1",
      status: { type: "running" },
    });
    expect(message?.content).toEqual([
      {
        type: "text",
        id: entries[0].id,
        text: "Hello world!",
        status: { type: "running" },
      },
    ]);
  });

  it("durable message arrival: durable KindAssistantMessage finalizes without duplicating content", () => {
    const snapPartial = ev(Kind.KindTextDelta, {
      id: "snap-turn-1-partial",
      turn_id: "turn-1",
      text: "Hello ",
    });

    const liveDelta = ev(Kind.KindTextDelta, {
      turn_id: "turn-1",
      text: "world!",
    });

    const durableMessage = ev(Kind.KindAssistantMessage, {
      id: "e-durable-asst-1",
      turn_id: "turn-1",
      text: "Hello world!",
    });

    const entries = fold(snapPartial, liveDelta, durableMessage);

    // Finalized in place: exactly 1 entry, streaming is false, text is finalized
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      turnID: "turn-1",
      text: "Hello world!",
      streaming: false,
    });

    const [message] = entriesToMessages(entries);

    expect(message).toMatchObject({
      role: "assistant",
      id: "turn-1",
      status: { type: "complete", reason: "stop" },
    });
    expect(message?.content).toEqual([
      {
        type: "text",
        id: entries[0].id,
        text: "Hello world!",
      },
    ]);
  });

  it("running tool catch-up: snap-<turnid>-tool-<callid> produces an active tool activity row", () => {
    const snapTool = ev(Kind.KindToolActivity, {
      id: "snap-turn-1-tool-call-101",
      turn_id: "turn-1",
      tool: {
        call_id: "call-101",
        name: "search",
        arguments: '{"query":"buddies"}',
        status: ToolStatus.ToolStatusRunning,
      },
    });

    const entries = fold(snapTool);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "tool",
      turnID: "turn-1",
      call: {
        call_id: "call-101",
        name: "search",
        arguments: '{"query":"buddies"}',
        status: ToolStatus.ToolStatusRunning,
      },
    });

    const [message] = entriesToMessages(entries);

    expect(message?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: entries[0].id,
        toolName: "search",
        args: { query: "buddies" },
        argsText: '{"query":"buddies"}',
        isError: false,
      },
    ]);
  });

  it("tool completion: terminal KindToolActivity resolves existing row by call_id", () => {
    const snapTool = ev(Kind.KindToolActivity, {
      id: "snap-turn-1-tool-call-101",
      turn_id: "turn-1",
      tool: {
        call_id: "call-101",
        name: "search",
        arguments: '{"query":"buddies"}',
        status: ToolStatus.ToolStatusRunning,
      },
    });

    const terminalTool = ev(Kind.KindToolActivity, {
      id: "e-durable-tool-101",
      turn_id: "turn-1",
      tool: {
        call_id: "call-101",
        name: "search",
        arguments: '{"query":"buddies"}',
        status: ToolStatus.ToolStatusCompleted,
        result: '{"items":["res1.ts","res2.ts"]}',
        duration_ms: 250,
      },
    });

    const entries = fold(snapTool, terminalTool);

    // Mutated in place by call_id: exactly 1 tool row exists
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "tool",
      turnID: "turn-1",
      call: {
        call_id: "call-101",
        name: "search",
        arguments: '{"query":"buddies"}',
        status: ToolStatus.ToolStatusCompleted,
        result: '{"items":["res1.ts","res2.ts"]}',
      },
    });

    const [message] = entriesToMessages(entries);

    expect(message?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: entries[0].id,
        toolName: "search",
        args: { query: "buddies" },
        argsText: '{"query":"buddies"}',
        isError: false,
        result: '{"items":["res1.ts","res2.ts"]}',
        timing: {
          startedAt: Date.parse("2026-01-01T00:00:00Z") - 250,
          completedAt: Date.parse("2026-01-01T00:00:00Z"),
        },
      },
    ]);
  });

  it("no snapshot events in durable transcript: reloading durable events contains no synthetic entries", () => {
    // Durable transcript replayed from persisted store (never includes snap- events)
    const durableEvents: SessionEvent[] = [
      ev(Kind.KindUserMessage, {
        id: "durable-msg-1",
        turn_id: "turn-1",
        text: "Search for files",
      }),
      ev(Kind.KindToolActivity, {
        id: "durable-tool-1",
        turn_id: "turn-1",
        tool: {
          call_id: "call-1",
          name: "search",
          arguments: '{"pattern":"*.go"}',
          status: ToolStatus.ToolStatusCompleted,
          result: '{"files":["main.go"]}',
        },
      }),
      ev(Kind.KindAssistantMessage, {
        id: "durable-msg-2",
        turn_id: "turn-1",
        text: "Found main.go",
      }),
      ev(Kind.KindTurnCompleted, {
        id: "durable-turn-done-1",
        turn_id: "turn-1",
      }),
    ];

    // Assert that durable transcript events carry genuine IDs and no synthetic snapshot prefix
    for (const event of durableEvents) {
      expect(event.id?.startsWith("snap-")).toBe(false);
    }

    const replayedEntries = fold(...durableEvents);

    // Replay creates standard entries without synthetic snapshot artifacts
    expect(replayedEntries).toHaveLength(3);
    expect(replayedEntries[0]).toMatchObject({
      kind: "user",
      text: "Search for files",
      turnID: "turn-1",
    });
    expect(replayedEntries[1]).toMatchObject({
      kind: "tool",
      turnID: "turn-1",
      call: {
        call_id: "call-1",
        status: ToolStatus.ToolStatusCompleted,
        result: '{"files":["main.go"]}',
      },
    });
    expect(replayedEntries[2]).toMatchObject({
      kind: "assistant",
      turnID: "turn-1",
      text: "Found main.go",
      streaming: false,
    });

    const messages = entriesToMessages(replayedEntries);

    expect(messages).toHaveLength(2);
    expect(messages[0]).toMatchObject({
      role: "user",
      content: [{ type: "text", text: "Search for files" }],
    });
    expect(messages[1]).toMatchObject({
      role: "assistant",
      status: { type: "complete", reason: "stop" },
    });
    expect(messages[1]?.content).toHaveLength(2);
  });

  it("handles a complete mid-stream catch-up lifecycle with running tool and text response", () => {
    // 1. Initial durable page contains the user prompt
    let entries = fold(
      ev(Kind.KindUserMessage, {
        id: "durable-user-1",
        turn_id: "turn-1",
        text: "Check repository status",
      })
    );

    // 2. Client subscribes mid-turn while tool is in-flight and receives running tool snapshot
    const snapTool = ev(Kind.KindToolActivity, {
      id: "snap-turn-1-tool-git-status",
      turn_id: "turn-1",
      tool: {
        call_id: "git-status-call",
        name: "execute",
        arguments: '{"command":"git status"}',
        status: ToolStatus.ToolStatusRunning,
      },
    });

    entries = applyEvent(entries, snapTool);

    expect(entries).toHaveLength(2);
    expect(entries[1]).toMatchObject({
      kind: "tool",
      turnID: "turn-1",
      call: {
        call_id: "git-status-call",
        status: ToolStatus.ToolStatusRunning,
      },
    });

    const [, midTurnMsg] = entriesToMessages(entries);

    expect(midTurnMsg?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: entries[1].id,
        toolName: "execute",
        args: { command: "git status" },
        argsText: '{"command":"git status"}',
        isError: false,
      },
    ]);

    // 3. Tool completes live
    const toolTerminal = ev(Kind.KindToolActivity, {
      id: "durable-tool-git-status",
      turn_id: "turn-1",
      tool: {
        call_id: "git-status-call",
        name: "execute",
        arguments: '{"command":"git status"}',
        status: ToolStatus.ToolStatusCompleted,
        result: "Clean working tree",
      },
    });

    entries = applyEvent(entries, toolTerminal);

    // Replaced in place
    expect(entries).toHaveLength(2);
    expect(entries[1]).toMatchObject({
      kind: "tool",
      call: {
        call_id: "git-status-call",
        result: "Clean working tree",
      },
    });

    // 4. Model produces live text delta following the tool result
    const postToolDelta = ev(Kind.KindTextDelta, {
      turn_id: "turn-1",
      text: "Working tree is clean.",
    });

    entries = applyEvent(entries, postToolDelta);

    expect(entries).toHaveLength(3);
    expect(entries[2]).toMatchObject({
      kind: "assistant",
      text: "Working tree is clean.",
      streaming: true,
    });

    // 5. Durable assistant message finalizes the trailing assistant entry
    const finalAsst = ev(Kind.KindAssistantMessage, {
      id: "durable-asst-1",
      turn_id: "turn-1",
      text: "Working tree is clean.",
    });

    entries = applyEvent(entries, finalAsst);

    expect(entries).toHaveLength(3);
    expect(entries[2]).toMatchObject({
      kind: "assistant",
      text: "Working tree is clean.",
      streaming: false,
    });

    // 6. Turn completes
    const turnDone = ev(Kind.KindTurnCompleted, {
      id: "durable-turn-done",
      turn_id: "turn-1",
    });

    entries = applyEvent(entries, turnDone);

    const [userMsg, assistantTurn] = entriesToMessages(entries);

    expect(userMsg).toMatchObject({ role: "user" });
    expect(assistantTurn).toMatchObject({
      role: "assistant",
      status: { type: "complete", reason: "stop" },
    });
    expect(assistantTurn?.content).toEqual([
      {
        type: "tool-call",
        toolCallId: entries[1].id,
        toolName: "execute",
        args: { command: "git status" },
        argsText: '{"command":"git status"}',
        isError: false,
        result: "Clean working tree",
      },
      {
        type: "text",
        id: entries[2].id,
        text: "Working tree is clean.",
      },
    ]);
  });

  it("store deduplication prevents double-folding duplicate snapshot events", () => {
    const snapPartial = ev(Kind.KindTextDelta, {
      id: "snap-turn-1-partial",
      turn_id: "turn-1",
      text: "Hello ",
    });

    const seenEventIDs = new Set<string>();

    const ingest = (current: Entry[], event: SessionEvent): Entry[] => {
      if (event.id) {
        if (seenEventIDs.has(event.id)) return current;
        seenEventIDs.add(event.id);
      }

      return applyEvent(current, event);
    };

    let entries: Entry[] = [];

    entries = ingest(entries, snapPartial);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello ",
      streaming: true,
    });

    // Duplicate snapshot delivery (e.g. rapid switch re-subscribe) is dropped by store dedupe
    entries = ingest(entries, snapPartial);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Hello ",
      streaming: true,
    });
  });
});
