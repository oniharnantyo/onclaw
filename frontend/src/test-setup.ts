/**
 * Test-environment globals jsdom does not implement. Vendored assistant-ui
 * registry sources (D13: never hand-edited) construct browser observers inside
 * layout effects — `SwapLabel` in `elements/surfaces.tsx` builds a
 * `ResizeObserver` to track its label width — so the constructor only has to
 * exist for those effects to run.
 *
 * The stub is deliberately a no-op: jsdom does no layout, so there is nothing
 * to observe and the effect performs its own initial measurement. Registered
 * through `setupFiles` in `vitest.config.ts`; no app entry imports this file,
 * so it never reaches the bundle.
 */
class ResizeObserverStub implements ResizeObserver {
  observe(): void {
    // No layout to observe under jsdom.
  }

  unobserve(): void {
    // Nothing was observed.
  }

  disconnect(): void {
    // Nothing was observed.
  }
}

globalThis.ResizeObserver ??= ResizeObserverStub;
