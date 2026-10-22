/**
 * Combobox tests (tasks.md 4.2 port): the trigger opens the popover with
 * ARIA combobox semantics, the search box filters, selecting fires onChange
 * with the option value, the hidden native select mirrors the value so tests
 * and forms can drive the picker like a plain select, keyboard navigation
 * works, and the error flag colors the trigger with the danger chain.
 */
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { Combobox } from "../Combobox";
import { renderUI } from "../../__test__/test-utils";

const OPTIONS = [
  { value: "gpt-5", label: "gpt-5", sublabel: "400k context" },
  { value: "gpt-4o", label: "gpt-4o" },
  { value: "locked", label: "Locked model", disabled: true },
];

describe("Combobox", () => {
  it("opens the popover, filters through the search box, and selects an option", async () => {
    const onChange = vi.fn();

    const { getByRole, getByTestId, unmount } = await renderUI(
      <Combobox
        options={OPTIONS}
        value=""
        onChange={onChange}
        placeholder="Select a model…"
      />
    );

    const trigger = getByRole("combobox");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");

    await act(async () => {
      fireEvent.click(trigger);
    });

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(getByRole("listbox")).toBeTruthy();
    expect(screen.getByText("Select a model…")).toBeTruthy();

    // Search narrows the list; the query matches sublabels too.
    const search = getByTestId("combobox-search-input");
    await act(async () => {
      fireEvent.change(search, { target: { value: "400k" } });
    });
    expect(screen.queryByTestId("combobox-option-gpt-4o")).toBeNull();
    expect(screen.getByTestId("combobox-option-gpt-5")).toBeTruthy();

    // Options select on mousedown (keeps the search input's focus).
    await act(async () => {
      fireEvent.mouseDown(screen.getByTestId("combobox-option-gpt-5"));
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith("gpt-5", OPTIONS[0]);
    expect(trigger.getAttribute("aria-expanded")).toBe("false");

    await unmount();
  });

  it("mirrors the value through the hidden native select (change-test seam)", async () => {
    const onChange = vi.fn();

    const { getByTestId, unmount } = await renderUI(
      <Combobox
        id="create-model"
        data-testid="select-create-model"
        options={OPTIONS}
        value=""
        onChange={onChange}
      />
    );

    // The visible trigger takes the derived test id; the native select keeps
    // the passed one and lists every option.
    expect(
      getByTestId("select-create-model-trigger").getAttribute("role")
    ).toBe("combobox");
    // SAFETY: with `id` passed, the test id lands on the hidden native
    // select, not the trigger button.
    const native = getByTestId("select-create-model") as HTMLSelectElement;
    expect(native.tagName).toBe("SELECT");
    expect(native.options).toHaveLength(OPTIONS.length);

    await act(async () => {
      fireEvent.change(native, { target: { value: "gpt-4o" } });
    });

    expect(onChange).toHaveBeenCalledWith("gpt-4o", OPTIONS[1]);

    await unmount();
  });

  it("navigates by keyboard and never selects a disabled option", async () => {
    const onChange = vi.fn();

    const { getByRole, getByTestId, unmount } = await renderUI(
      <Combobox options={OPTIONS} value="gpt-4o" onChange={onChange} />
    );

    const trigger = getByRole("combobox");

    await act(async () => {
      fireEvent.keyDown(trigger, { key: "ArrowDown" });
    });
    expect(trigger.getAttribute("aria-expanded")).toBe("true");

    // The active option starts on the current selection.
    const listbox = getByTestId("combobox-options-list");
    const active = listbox.querySelector('[aria-selected="true"]');
    expect(active?.getAttribute("data-value")).toBe("gpt-4o");

    // One ArrowDown lands on the disabled option; Enter must not select it.
    await act(async () => {
      fireEvent.keyDown(getByTestId("combobox-search-input"), {
        key: "ArrowDown",
      });
    });

    await act(async () => {
      fireEvent.keyDown(getByTestId("combobox-search-input"), { key: "Enter" });
    });
    expect(onChange).not.toHaveBeenCalled();

    // Escape closes without selecting.
    await act(async () => {
      fireEvent.keyDown(getByTestId("combobox-search-input"), {
        key: "Escape",
      });
    });
    await waitFor(() =>
      expect(trigger.getAttribute("aria-expanded")).toBe("false")
    );

    await unmount();
  });

  it("shows the empty text for unmatched queries and colors an error trigger with the danger chain", async () => {
    const { getByRole, getByTestId, getByText, unmount } = await renderUI(
      <Combobox
        options={OPTIONS}
        value=""
        onChange={() => {}}
        error
        emptyText="No models found"
      />
    );

    await act(async () => {
      fireEvent.click(getByRole("combobox"));
    });

    expect(getByRole("combobox").className).toContain("border-danger");

    await act(async () => {
      fireEvent.change(getByTestId("combobox-search-input"), {
        target: { value: "zzz" },
      });
    });
    expect(getByText("No models found")).toBeTruthy();

    await unmount();
  });
});
