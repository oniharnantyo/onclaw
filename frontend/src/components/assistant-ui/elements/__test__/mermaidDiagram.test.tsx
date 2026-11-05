/**
 * MermaidDiagram lazy-renderer tests (fix-ui-audit-findings design D6): the
 * element paints the fenced source immediately and swaps in the diagram when
 * the beautiful-mermaid chunk lands. The heavy module is mocked at its
 * boundary behind a gate, so the placeholder state is observable.
 */
import { act } from "react";
import { waitFor } from "@testing-library/dom";
import { describe, expect, it, vi } from "vite-plus/test";

import { MermaidDiagram } from "../mermaid-diagram";
import { renderUI } from "../../../../__test__/test-utils";

const gate = vi.hoisted(() => {
  let release = (): void => undefined;
  const opened = new Promise<void>((resolve) => {
    release = resolve;
  });

  return { opened, release };
});

const { renderMermaidSVG } = vi.hoisted(() => ({
  renderMermaidSVG: vi.fn(
    (code: string): string => `<svg data-test-mermaid="1">${code}</svg>`
  ),
}));

vi.mock("beautiful-mermaid", async () => {
  await gate.opened;

  return { renderMermaidSVG };
});

const SOURCE = "graph TD; A-->B";

describe("MermaidDiagram lazy renderer (design D6)", () => {
  it("paints the code fence until the renderer chunk lands, then the diagram", async () => {
    const { container, unmount } = await renderUI(
      <MermaidDiagram code={SOURCE} />
    );

    const pending = container.querySelector("[data-slot='mermaid-pending']");

    expect(pending).not.toBeNull();
    expect(pending?.textContent).toContain(SOURCE);
    expect(renderMermaidSVG).not.toHaveBeenCalled();

    await act(async () => {
      gate.release();
    });

    await waitFor(() => {
      expect(container.querySelector("[data-test-mermaid]")).not.toBeNull();
    });

    expect(renderMermaidSVG).toHaveBeenCalled();
    expect(container.querySelector("[data-slot='mermaid-pending']")).toBeNull();

    await unmount();
  });
});