/**
 * Group transcript mapping tests (add-agent-groups task 5.2, design D7):
 * every AgentGroupMessage row shape folds into the shared Entry union with
 * its per-entry authorship — member rows carry their agent id (avatar + name
 * resolve at render time), coordinator rows carry the system marker, user
 * rows map to the ordinary user entry with no attribution, and the
 * renderer's message-id seam resolves the author back off the mapped
 * entries.
 */
import { describe, expect, it } from "vite-plus/test";

import type { AgentGroupMessage } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { authorOfMessage, mapGroupMessages } from "../groupEntries";
import type { Entry } from "../../chat/types";

function row(extra: Partial<AgentGroupMessage>): AgentGroupMessage {
  return {
    GroupID: "g1",
    MessageID: "m1",
    Seq: 1,
    Kind: "message",
    AuthorKind: "user",
    AuthorAgentID: null,
    TaskID: null,
    Payload: JSON.stringify({ text: "hello" }),
    OccurredAt: "2026-01-01T00:00:00Z",
    ...extra,
  };
}

describe("mapGroupMessages", () => {
  it("maps a member reply to an assistant entry attributed to that member", () => {
    const entries = mapGroupMessages([
      row({
        MessageID: "turn-9",
        AuthorKind: "agent",
        AuthorAgentID: "a2",
        Payload: JSON.stringify({ text: "scout's answer" }),
      }),
    ]);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      id: "turn-9",
      turnID: "turn-9",
      text: "scout's answer",
      streaming: false,
      author: { kind: "member", agentID: "a2" },
    });
  });

  it("attributes a task-linked member row copied back while a task ran", () => {
    const entries = mapGroupMessages([
      row({
        AuthorKind: "agent",
        AuthorAgentID: "a2",
        TaskID: "t9",
      }),
    ]);

    expect(entries[0]).toMatchObject({
      author: { kind: "member", agentID: "a2" },
    });
  });

  it("maps a coordinator assignment line to a system row", () => {
    const entries = mapGroupMessages([
      row({
        Kind: "assigned",
        AuthorKind: "coordinator",
        TaskID: "t1",
        Payload: JSON.stringify({ text: "Assigned to Scout" }),
      }),
    ]);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "Assigned to Scout",
      author: { kind: "coordinator" },
    });
  });

  it("maps a coordinator change line to a system row", () => {
    const entries = mapGroupMessages([
      row({
        Kind: "change",
        AuthorKind: "coordinator",
        Payload: JSON.stringify({ text: "added Scout" }),
      }),
    ]);

    expect(entries[0]).toMatchObject({
      author: { kind: "coordinator" },
    });
  });

  it("maps a coordinator error row to the system row voice, not an error card", () => {
    const entries = mapGroupMessages([
      row({
        Kind: "error",
        AuthorKind: "coordinator",
        Payload: JSON.stringify({ text: "task failed: boom" }),
      }),
    ]);

    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "task failed: boom",
      author: { kind: "coordinator" },
    });
  });

  it("maps a user row to the ordinary user entry without attribution", () => {
    const entries = mapGroupMessages([row({})]);

    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      kind: "user",
      id: "m1",
      text: "hello",
    });
    // SAFETY: only the group mapping mints authors; the widened read keeps
    // the union access legal (the entries.ts seam convention).
    expect((entries[0] as { author?: unknown }).author).toBeUndefined();
  });

  it("rides the user row's attachments into the user entry", () => {
    const entries = mapGroupMessages([
      row({
        Payload: JSON.stringify({
          text: "",
          attachments: [
            { key: "k1", name: "shot.png", mime: "image/png", size: 9 },
          ],
        }),
      }),
    ]);

    expect(entries[0]).toMatchObject({
      kind: "user",
      text: "",
      attachments: [
        { key: "k1", name: "shot.png", mime: "image/png", size: 9 },
      ],
    });
  });

  it("maps a task result row to the reporting member", () => {
    const entries = mapGroupMessages([
      row({
        Kind: "result",
        AuthorKind: "agent",
        AuthorAgentID: "a2",
        TaskID: "t1",
        Payload: JSON.stringify({ text: "the corpus is summarized" }),
      }),
    ]);

    expect(entries[0]).toMatchObject({
      kind: "assistant",
      text: "the corpus is summarized",
      author: { kind: "member", agentID: "a2" },
    });
  });

  it("skips rows with nothing to render", () => {
    const entries = mapGroupMessages([
      row({ MessageID: "m-empty", Payload: "" }),
      row({ MessageID: "m-blank", Payload: JSON.stringify({ text: "" }) }),
      row({ MessageID: "m-full", Payload: JSON.stringify({ text: "kept" }) }),
    ]);

    expect(entries.map((e: Entry): string => e.id)).toEqual(["m-full"]);
  });

  it("keeps transcript order and stable ids across a re-map", () => {
    const rows = [
      row({ MessageID: "m1", Seq: 1 }),
      row({
        MessageID: "m2",
        Seq: 2,
        AuthorKind: "agent",
        AuthorAgentID: "a2",
      }),
    ];

    const first = mapGroupMessages(rows);
    const second = mapGroupMessages(rows);

    expect(first.map((e: Entry): string => e.id)).toEqual(["m1", "m2"]);
    expect(second.map((e: Entry): string => e.id)).toEqual(["m1", "m2"]);
  });
});

describe("the renderer's message-id seam", () => {
  const entries = mapGroupMessages([
    row({ MessageID: "u1", Seq: 1 }),
    row({
      MessageID: "a1",
      Seq: 2,
      AuthorKind: "agent",
      AuthorAgentID: "a2",
      TaskID: "t1",
    }),
    row({
      MessageID: "c1",
      Seq: 3,
      Kind: "assigned",
      AuthorKind: "coordinator",
      TaskID: "t2",
    }),
  ]);

  it("resolves a member row's author by the message id", () => {
    expect(authorOfMessage(entries, "a1")).toEqual({
      kind: "member",
      agentID: "a2",
    });
  });

  it("resolves a coordinator row by the message id", () => {
    expect(authorOfMessage(entries, "c1")).toEqual({ kind: "coordinator" });
  });

  it("resolves no author for a user row and nothing for an unknown id", () => {
    expect(authorOfMessage(entries, "u1")).toBeNull();
    expect(authorOfMessage(entries, "nope")).toBeNull();
    expect(authorOfMessage(entries, "")).toBeNull();
  });
});
