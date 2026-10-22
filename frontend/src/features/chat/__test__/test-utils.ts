import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  getQueriesForElement,
  queries,
  type BoundFunctions,
} from "@testing-library/dom";
import { afterEach } from "vite-plus/test";

// React 18 requires this flag for act() to flush synchronously outside
// react-dom/test-utils.
// SAFETY: globalThis always exists and the flag is an optional boolean.
(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

// Rendered roots, auto-unmounted after each test so screen queries never see
// a previous test's DOM.
const roots: Root[] = [];

const containers: HTMLElement[] = [];

type RootRef = { current: Root | null };

type Bound = BoundFunctions<typeof queries>;

type RenderResult = { [K in keyof Bound]: Bound[K] } & {
  container: HTMLElement;
  unmount: () => Promise<void>;
  rerender: (ui: React.ReactElement) => Promise<void>;
};

afterEach(() => {
  while (roots.length) {
    const root = roots.pop()!;
    act(() => {
      root.unmount();
    });
  }

  while (containers.length) {
    containers.pop()!.remove();
  }

  document.body.innerHTML = "";
});

/**
 * Casts a mock-client literal to the suite's mocked Client shape: the
 * suites mock only the members they drive, so the Client-typed intersection
 * defeats the structural check and every suite funnels through this one
 * audited bridge instead of hand-rolled assertion chains.
 */
export function mockedSurface<S extends object, L extends object>(mock: L): S {
  // SAFETY: the literal covers exactly the surface the suite drives; this
  // bridge only relabels the object for the compiler, the runtime value is
  // unchanged.
  const wide = mock as object;

  // SAFETY: as above — the relabeled object carries the mocked members.
  return wide as S;
}

/**
 * Minimal render helper over react-dom so chat tests avoid a hard dependency
 * on @testing-library/react (not installed); queries come from
 * @testing-library/dom bound to the rendered container. Mirrors
 * features/settings/test-utils.ts.
 */
export async function renderUI(ui: React.ReactElement): Promise<RenderResult> {
  const container = document.createElement("div");
  document.body.appendChild(container);
  containers.push(container);
  const root: RootRef = { current: null };
  await act(async () => {
    root.current = createRoot(container);
    root.current!.render(ui);
  });
  roots.push(root.current!);

  return {
    container,
    ...getQueriesForElement(container),
    unmount: async (): Promise<void> => {
      await act(async () => {
        root.current!.unmount();
      });
      container.remove();
    },
    rerender: async (ui: React.ReactElement): Promise<void> => {
      await act(async () => {
        root.current!.render(ui);
      });
    },
  };
}
