/**
 * The Telegram-origin badge on the transcript's user rows (add-telegram-channel
 * task 9.2): a replayed prompt whose persisted event carries origin "telegram"
 * renders the small paper-plane chip, a user-origin prompt renders bare, and a
 * legacy row persisted before origins were recorded (empty origin) renders
 * bare too — the badge names only channel turns.
 */
import { screen } from "@testing-library/dom";
import { beforeEach, describe, expect, it } from "vite-plus/test";

import { Kind } from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { applyEvent, type Entry } from "../../entries";
import { useChatStore } from "../../store";
import { ev, renderThread } from "./harness";

beforeEach(() => {
  useChatStore.setState({ busy: false, activeSessionID: "s1", entries: [] });
});

describe("telegram-origin badge", () => {
  it("badges a telegram-origin prompt", async () => {
    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "hello from the channel",
        origin: "telegram",
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    expect(screen.getByTestId("origin-telegram").textContent).toContain(
      "Telegram"
    );
    expect(screen.getByTestId("user-text").textContent).toContain(
      "hello from the channel"
    );

    await h.unmount();
  });

  it("a user-origin prompt renders bare", async () => {
    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "plain chat",
        origin: "user",
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    expect(screen.getByTestId("user-text").textContent).toContain("plain chat");
    expect(screen.queryByTestId("origin-telegram")).toBeNull();

    await h.unmount();
  });

  it("a legacy row with no origin renders bare", async () => {
    const entries: Entry[] = applyEvent(
      [],
      ev(Kind.KindUserMessage, {
        turn_id: "t1",
        id: "e1",
        text: "before origins existed",
      })
    );

    const h = await renderThread(entries);

    await h.settle();

    expect(screen.queryByTestId("origin-telegram")).toBeNull();

    await h.unmount();
  });
});
