/**
 * MarkdownBody tests (D6, spec merge-right-bar-previews): the standalone
 * body renders a bare string through the transcript's pipeline — GFM tables
 * and lists come through the text prop, the wrapper carries the `aui-md`
 * class, the body re-renders when the text changes, and inline code keeps
 * the transcript's `aui-md-inline-code` styling — all without any message
 * context.
 */
import { act, useState } from "react";
import { describe, expect, it } from "vite-plus/test";

import { MarkdownBody } from "../MarkdownBody";
import { renderUI } from "../../../__test__/test-utils";

// The flip is set by the probe below and driven by the re-render test
// (the shared components test-utils render helper exposes no rerender).
let flipProbe: () => void = () => undefined;

/** Renders `second`'s text only after the test flips it, so the change
 * flows through React's update path rather than a remount. */
function TextProbe({
  first,
  second,
}: {
  first: string;
  second: string;
}): React.ReactElement {
  const [useSecond, setUseSecond] = useState(false);
  flipProbe = () => setUseSecond(true);

  return <MarkdownBody text={useSecond ? second : first} />;
}

describe("MarkdownBody", () => {
  it("renders a GFM table and a list from the text prop", async () => {
    const { container, unmount } = await renderUI(
      <MarkdownBody
        text={["| A | B |", "| - | - |", "| 1 | 2 |", "", "- first", "- second"].join(
          "\n"
        )}
      />
    );

    expect(container.querySelector("table")).toBeTruthy();
    expect(container.querySelectorAll("th")).toHaveLength(2);
    expect(container.querySelectorAll("td")).toHaveLength(2);
    expect(container.querySelector("ul")?.className).toContain("aui-md-ul");
    expect(container.querySelectorAll("li")).toHaveLength(2);
    expect(container.querySelectorAll("li")[0]!.textContent).toBe("first");

    await unmount();
  });

  it("carries the aui-md class on its wrapper", async () => {
    const { container, unmount } = await renderUI(
      <MarkdownBody text="hello" />
    );

    expect(container.firstElementChild!.className).toContain("aui-md");

    await unmount();
  });

  it("re-renders when the text prop changes", async () => {
    const { container, unmount } = await renderUI(
      <TextProbe first="before" second="after" />
    );

    expect(container.textContent).toBe("before");

    await act(async () => {
      flipProbe();
    });

    expect(container.textContent).toBe("after");

    await unmount();
  });

  it("styles inline code through the transcript's inline-code chain", async () => {
    const { container, unmount } = await renderUI(
      <MarkdownBody text="run `npm test` now" />
    );

    const code = container.querySelector("code");

    expect(code).toBeTruthy();
    expect(code!.className).toContain("aui-md-inline-code");
    expect(code!.textContent).toBe("npm test");

    await unmount();
  });
});
