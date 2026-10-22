/**
 * Component tests for the AvatarPicker (avatar parity): Randomize emits a
 * fresh full config, Customize toggles the trait grid, and a trait edit
 * emits the full config with only the touched key changed — the exact shape
 * the agent row stores and every surface renders back. Mounted through the
 * shared renderUI helper because @testing-library/react is not a project
 * dependency.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";
import type { AvatarFullConfig } from "react-nice-avatar";

import { renderUI } from "../../../__test__/test-utils";
import { AvatarPicker } from "../components/AvatarPicker";

describe("AvatarPicker", () => {
  it("randomizes to a fresh full config", async () => {
    const onChange = vi.fn();

    const { unmount } = await renderUI(
      <AvatarPicker value={{ sex: "man" }} onChange={onChange} />
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-avatar-randomize"));
    });

    expect(onChange).toHaveBeenCalledTimes(1);

    // SAFETY: vi.fn() calls carry the config as the only argument.
    const config = onChange.mock.calls[0][0] as AvatarFullConfig;

    // genConfig fills every trait — a roll is a complete config, not a patch.
    expect(Object.keys(config).length).toBeGreaterThan(5);

    await unmount();
  });

  it("toggles the customize grid", async () => {
    const { container, unmount } = await renderUI(
      <AvatarPicker value={{ sex: "man" }} onChange={vi.fn()} />
    );

    expect(container.querySelectorAll("select").length).toBe(0);

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-avatar-customize"));
    });

    // Nine trait selects (the five color traits render as swatch rows).
    expect(container.querySelectorAll("select").length).toBe(9);

    await act(async () => {
      fireEvent.click(screen.getByTestId("btn-avatar-customize"));
    });

    expect(container.querySelectorAll("select").length).toBe(0);

    await unmount();
  });

  it("changes one trait and keeps the rest of the config", async () => {
    const onChange = vi.fn();

    const { container, unmount } = await renderUI(
      <AvatarPicker
        value={{ sex: "man", mouthStyle: "smile" }}
        onChange={onChange}
      />
    );

    // Scoped to this mount so a leftover from a failed test never
    // double-matches through document-level queries.
    // SAFETY: the picker always renders the customize toggle.
    const customize = container.querySelector(
      "[data-testid='btn-avatar-customize']"
    );

    await act(async () => {
      fireEvent.click(customize!);
    });

    // SAFETY: the grid's first trait select is Sex.
    const sexSelect = container.querySelectorAll(
      "select"
    )[0] as HTMLSelectElement;

    await act(async () => {
      fireEvent.change(sexSelect, { target: { value: "woman" } });
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ sex: "woman", mouthStyle: "smile" })
    );

    await unmount();
  });
});
