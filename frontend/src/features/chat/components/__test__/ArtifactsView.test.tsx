/**
 * ArtifactsView tests (add-turn-artifacts tasks 6.3, spec chat-artifacts
 * "Artifacts chips row"): each chip shows the name and the human-readable
 * size with the change kind as a tooltip only when it is known, nine
 * artifacts collapse to six inline chips behind a "+3 more" disclosure that
 * reveals the rest, a truncated list states the cap, activating a chip calls
 * onOpen with the full workspace-relative path, chips without an onOpen
 * render non-interactive, and zero artifacts render no row at all.
 */
import { act } from "react";
import { fireEvent, screen } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import {
  ArtifactChange,
  type Artifact,
} from "../../../../../bindings/github.com/oniharnantyo/buddies/internal/sessions/models";
import { renderUI } from "../../__test__/test-utils";
import { ArtifactsView } from "../ArtifactsView";

function artifact(
  path: string,
  size: number,
  change?: ArtifactChange
): Artifact {
  return change === undefined ? { path, size } : { path, size, change };
}

describe("ArtifactsView (add-turn-artifacts task 6.3, design D7)", () => {
  it("renders each chip's name and size with the full path in the tooltip", async () => {
    await renderUI(
      <ArtifactsView
        artifacts={[artifact("artifacts/report.md", 1234)]}
        onOpen={(): void => undefined}
      />
    );

    expect(screen.getByTestId("entry-artifacts")).toBeTruthy();

    const chip = screen.getByRole("button", { name: "report.md" });

    expect(chip.textContent).toContain("report.md");
    expect(chip.textContent).toContain("1.2 KB");
    expect(chip.getAttribute("title")).toBe("artifacts/report.md · 1.2 KB");
  });

  it("carries the change kind in the tooltip only when it is known", async () => {
    await renderUI(
      <ArtifactsView
        artifacts={[
          artifact("artifacts/new.md", 10, ArtifactChange.ArtifactCreated),
          artifact("artifacts/old.md", 20, ArtifactChange.ArtifactModified),
          artifact("artifacts/plain.md", 30),
        ]}
        onOpen={(): void => undefined}
      />
    );

    expect(
      screen.getByRole("button", { name: "new.md" }).getAttribute("title")
    ).toContain("Created · 10 B");
    expect(
      screen.getByRole("button", { name: "old.md" }).getAttribute("title")
    ).toContain("Modified · 20 B");
    expect(
      screen.getByRole("button", { name: "plain.md" }).getAttribute("title")
    ).not.toMatch(/Created|Modified/);
  });

  it("collapses nine artifacts behind a +3 more disclosure that reveals the rest", async () => {
    const nine: Artifact[] = [];

    for (let i = 1; i <= 9; i += 1) {
      nine.push(artifact(`artifacts/f${i}.md`, 10));
    }

    await renderUI(<ArtifactsView artifacts={nine} />);

    expect(screen.queryAllByTestId("artifact-chip")).toHaveLength(6);

    const more = screen.getByTestId("artifacts-more");

    expect(more.textContent).toBe("+3 more");

    await act(async () => {
      fireEvent.click(more);
    });

    expect(screen.queryAllByTestId("artifact-chip")).toHaveLength(9);
    expect(screen.queryByTestId("artifacts-more")).toBeNull();
  });

  it("notes when the backend truncated the list at its cap", async () => {
    await renderUI(
      <ArtifactsView artifacts={[artifact("artifacts/a.md", 5)]} truncated />
    );

    expect(screen.getByTestId("artifacts-truncated").textContent).toContain(
      "first 50"
    );
  });

  it("activating a chip calls onOpen with the full workspace-relative path", async () => {
    const onOpen = vi.fn<(path: string) => void>();

    await renderUI(
      <ArtifactsView
        artifacts={[artifact("artifacts/nested/report.md", 5)]}
        onOpen={onOpen}
      />
    );

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "nested/report.md" }));
    });

    expect(onOpen).toHaveBeenCalledWith("artifacts/nested/report.md");
  });

  it("renders non-interactive chips without an onOpen", async () => {
    const { container } = await renderUI(
      <ArtifactsView artifacts={[artifact("artifacts/a.md", 5)]} />
    );

    expect(screen.getByTestId("artifact-chip").tagName).toBe("SPAN");
    expect(container.querySelector("button")).toBeNull();
  });

  it("renders no row for zero artifacts", async () => {
    await renderUI(<ArtifactsView artifacts={[]} />);

    expect(screen.queryByTestId("entry-artifacts")).toBeNull();
  });
});
