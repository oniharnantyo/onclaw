import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  getQueriesForElement,
  queries,
  type BoundFunctions,
} from "@testing-library/dom";

// React 18 requires this flag for act() to flush synchronously outside
// react-dom/test-utils.
// SAFETY: globalThis always exists and the flag is an optional boolean.
(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

type RootRef = { current: Root | null };

type Bound = BoundFunctions<typeof queries>;

type RenderResult = { [K in keyof Bound]: Bound[K] } & {
  container: HTMLElement;
  unmount: () => Promise<void>;
};

/**
 * Minimal render helper over react-dom so component tests avoid a hard
 * dependency on @testing-library/react (not installed); queries come from
 * @testing-library/dom bound to the rendered container.
 */
export async function renderUI(ui: React.ReactElement): Promise<RenderResult> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root: RootRef = { current: null };
  await act(async () => {
    root.current = createRoot(container);
    root.current!.render(ui);
  });

  return {
    container,
    ...getQueriesForElement(container),
    unmount: async (): Promise<void> => {
      await act(async () => {
        root.current!.unmount();
      });
      container.remove();
    },
  };
}
