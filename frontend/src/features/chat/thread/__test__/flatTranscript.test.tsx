/**
 * Flat transcript tests (spec chat-view "Chat transcript rendering": the
 * flat reading surface): the reasoning renders as one quiet disclosure line
 * with no surface of its own, a run of fewer than four tool calls renders
 * inline in the transcript flow with no group disclosure standing in for it,
 * and every assistant turn opens with a rule while the answer and the rows
 * above it share one left edge and one measure.
 */
import { act } from "react";
import { screen, waitFor } from "@testing-library/dom";
import { describe, expect, it } from "vite-plus/test";

import {
  Kind,
  ToolStatus,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import type { Entry } from "../../entries";
import { ev, fold, renderThread } from "./harness";

/** One completed tool call inside a turn. */
function toolCall(
  turnID: string,
  callID: string,
  file: string
): ReturnType<typeof ev> {
  return ev(Kind.KindToolActivity, {
    turn_id: turnID,
    tool: {
      call_id: callID,
      name: "read_file",
      arguments: `{"file_path":"${file}"}`,
      status: ToolStatus.ToolStatusCompleted,
      result: "contents",
    },
  });
}

/** The entry id a call's card renders under: the mapping's part ids are the
 * entry ids, not the session's call ids. */
function cardID(entries: Entry[], callID: string): string {
  const entry = entries.find(
    (candidate: Entry): boolean =>
      candidate.kind === "tool" && candidate.call?.call_id === callID
  );

  if (entry === undefined) throw new Error(`no tool entry for ${callID}`);

  return entry.id;
}

describe("the flat transcript (spec chat-view 'Chat transcript rendering')", () => {
  it("renders the reasoning as one quiet disclosure line, with no surface of its own", async () => {
    const h = await renderThread(
      fold(
        ev(Kind.KindUserMessage, { text: "think", turn_id: "t1" }),
        ev(Kind.KindReasoning, {
          text: "weighing the options",
          turn_id: "t1",
        }),
        ev(Kind.KindAssistantMessage, { text: "the answer", turn_id: "t1" })
      )
    );

    const root = document.querySelector('[data-slot="reasoning-root"]');
    const trigger = document.querySelector('[data-slot="reasoning-trigger"]');

    expect(trigger).toBeTruthy();
    expect(trigger?.textContent).toContain("Reasoning");
    // The brain glyph and the disclosure control ride the line.
    expect(
      document.querySelector('[data-slot="reasoning-trigger-icon"]')
    ).toBeTruthy();
    expect(
      document.querySelector('[data-slot="reasoning-trigger-chevron"]')
    ).toBeTruthy();

    // The group carries no panel of its own…
    expect(root?.className).not.toContain("border");
    expect(root?.className).not.toContain("rounded");
    expect(root?.className).not.toContain("bg-");
    // …and it breathes: the line is not glued to the prose above it.
    expect(root?.className).toContain("my-2");
    // …and the line itself is muted.
    expect(trigger?.className).toContain("text-muted-foreground");

    // Collapsed: the thought is not on the page yet.
    expect(document.body.textContent).not.toContain("weighing the options");

    await act(async () => {
      // SAFETY: the trigger is the reasoning disclosure button the element
      // rendered; the assertion only narrows it to a clickable element.
      (trigger as HTMLElement).click();
    });

    await waitFor(() =>
      expect(document.body.textContent).toContain("weighing the options")
    );

    // Revealed, the thought is indented beneath the line.
    const text = document.querySelector('[data-slot="reasoning-text"]');

    expect(text?.className).toContain("ps-3");

    await h.unmount();
  });

  it("renders a short tool run inline, in the flow, with no group disclosure", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "look", turn_id: "t1" }),
      toolCall("t1", "c1", "src/a.ts"),
      toolCall("t1", "c2", "src/b.ts")
    );

    const h = await renderThread(entries);

    // The run sits in the transcript flow…
    expect(screen.getByTestId("run-inline")).toBeTruthy();
    // …with air around the block (the rows stay tight with each other).
    expect(screen.getByTestId("run-inline").className).toContain("my-2");
    expect(screen.queryByTestId("run-summary")).toBeNull();
    // …with no group disclosure standing in for it.
    expect(
      document.querySelector('[data-slot="tool-group-trigger"]')
    ).toBeNull();

    // Both calls are on the page, in message order.
    const first = screen.getByTestId(`entry-tool-${cardID(entries, "c1")}`);
    const second = screen.getByTestId(`entry-tool-${cardID(entries, "c2")}`);

    expect(
      first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();

    await h.unmount();
  });

  it("separates the assistant turns with a rule and keeps one left edge and one measure", async () => {
    const entries = fold(
      ev(Kind.KindUserMessage, { text: "first", turn_id: "t1" }),
      ev(Kind.KindAssistantMessage, { text: "answer one", turn_id: "t1" }),
      ev(Kind.KindTurnCompleted, { turn_id: "t1" }),
      ev(Kind.KindUserMessage, { text: "second", turn_id: "t2" }),
      ev(Kind.KindAssistantMessage, { text: "answer two", turn_id: "t2" }),
      toolCall("t2", "c1", "src/a.ts")
    );

    const h = await renderThread(entries);

    // A rule opens each assistant turn.
    expect(screen.getAllByTestId("turn-rule")).toHaveLength(2);

    // The answer is neither capped nor inset: the prose and the rows above
    // it share the column's left edge and its measure.
    const assistant = screen.getAllByTestId("entry-assistant")[0];

    expect(assistant.className).not.toContain("max-w");
    expect(assistant.className).not.toContain("px-");

    const inline = screen.getByTestId("run-inline");

    expect(inline.className).not.toContain("px-");

    await h.unmount();
  });
});
