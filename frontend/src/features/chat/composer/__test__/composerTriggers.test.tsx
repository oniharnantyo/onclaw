/**
 * Trigger menus suite (task 5.2): the three composer popovers — the `/` slash
 * catalogue, the `@` agent mentions and the `$` skills. Each opens on its
 * trigger character, filters as the user types, inserts the token literally on
 * selection, and dismisses on Escape.
 *
 * The detection mirrors the legacy composer's rule: the slash menu only opens
 * at the very start of the text, the mention and skill menus open from the
 * nearest trigger character with no whitespace before the caret, and the
 * nearest trigger character wins (so `$foo` never opens the mention menu).
 */
import { act } from "react";
import { screen, waitFor } from "@testing-library/dom";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vite-plus/test";

// The client mock factory is imported FIRST on purpose: the hoisted
// `vi.mock` below runs the moment the chat store pulls the client module in,
// so the factory's module must already be evaluated by then.
import { makeClient } from "./clientMock";
import { renderComposer, seedStore } from "./harness";

vi.mock("../../../../lib/client", () => ({ client: makeClient() }));

import { useChatStore } from "../../store";

/** The open trigger popover, if any. */
function popover(): HTMLElement | null {
  return document.querySelector('[data-slot="composer-trigger-popover"]');
}

/** The popover's item list. */
function items(): HTMLElement | null {
  return document.querySelector('[data-slot="composer-trigger-popover-items"]');
}

/** The item whose label matches, inside the open popover. */
function item(label: string): HTMLElement {
  const list = items();

  if (list === null) throw new Error("no trigger popover is open");

  // The options are the role="option" rows; their wrappers carry the same
  // text, so the role is what picks the clickable one. The label is a
  // substring of the row (a row may also carry a description).
  const found = Array.from(
    list.querySelectorAll<HTMLElement>('[role="option"]')
  ).find((element: HTMLElement): boolean =>
    (element.textContent ?? "").includes(label)
  );

  if (found === undefined) throw new Error(`trigger item "${label}" not found`);

  return found;
}

/** The composer's textarea. */
function input(): HTMLTextAreaElement {
  // SAFETY: the chat composer renders as a textarea by contract.
  return screen.getByTestId("chat-composer") as HTMLTextAreaElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  seedStore();
});

describe("the trigger menus (task 5.2)", () => {
  it("the slash menu opens on / and filters as the user types", async () => {
    const user = userEvent.setup();
    await renderComposer();

    expect(popover()).toBeNull();

    await user.type(input(), "/");

    await waitFor(() => expect(popover()).not.toBeNull());
    expect(items()?.textContent).toContain("/new");

    // Filtering: a query that matches nothing empties the list.
    await user.type(input(), "zz");
    await waitFor(() => expect(items()?.textContent).not.toContain("/new"));

    // Back to the match: the option is back.
    await user.type(input(), "{backspace}{backspace}");
    await waitFor(() => expect(items()?.textContent).toContain("/new"));
  });

  it("a slash pick inserts the command and the menu dismisses", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "/");
    await waitFor(() => expect(popover()).not.toBeNull());

    await act(async () => {
      item("/new").click();
    });

    await waitFor(() => expect(input().value).toContain("/new"));
    await waitFor(() => expect(popover()).toBeNull());
  });

  it("the mention menu opens on @ and lists the agents", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "@");

    await waitFor(() => expect(popover()).not.toBeNull());
    expect(items()?.textContent).toContain("Scout");
    expect(items()?.textContent).toContain("Runner");
  });

  it("a mention pick inserts the agent name and the menu dismisses", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "@Sco");
    await waitFor(() => expect(popover()).not.toBeNull());

    await act(async () => {
      item("Scout").click();
    });

    await waitFor(() => expect(input().value).toContain("@Scout"));
    await waitFor(() => expect(popover()).toBeNull());
  });

  it("the skill menu opens on $ and lists the enabled skills", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "$");

    await waitFor(() => expect(popover()).not.toBeNull());
    expect(items()?.textContent).toContain("search");
    // The disabled skill never reaches the store, so no option exists for it.
    expect(items()?.textContent).not.toContain("secret");
  });

  it("a skill pick inserts the skill name and the menu dismisses", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "$se");
    await waitFor(() => expect(popover()).not.toBeNull());

    await act(async () => {
      item("search").click();
    });

    await waitFor(() => expect(input().value).toContain("$search"));
    await waitFor(() => expect(popover()).toBeNull());
  });

  it("the nearest trigger character wins: $ does not open the mention menu", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "$search ");

    // A space closes the trigger: no menu is open.
    await waitFor(() => expect(popover()).toBeNull());
  });

  it("Escape dismisses the open menu", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "/");
    await waitFor(() => expect(popover()).not.toBeNull());

    // The popover owns the keyboard while it is open: Escape closes it.
    await user.keyboard("{Escape}");

    await waitFor(() => expect(popover()).toBeNull());
  });

  it("a send never fires while a trigger menu is open", async () => {
    const user = userEvent.setup();
    await renderComposer();

    await user.type(input(), "/");
    await waitFor(() => expect(popover()).not.toBeNull());

    // Enter is owned by the open popover: it picks the highlighted item
    // instead of sending the message.
    await user.keyboard("{Enter}");

    await waitFor(() => expect(input().value).toContain("/new"));
    expect(useChatStore.getState().queue).toHaveLength(0);
  });
});
