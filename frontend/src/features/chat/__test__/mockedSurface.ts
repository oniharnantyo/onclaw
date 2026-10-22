/**
 * Casts a mock-client literal to the suite's mocked Client shape: the
 * suites mock only the members they drive, so the Client-typed intersection
 * defeats the structural check and every suite funnels through this one
 * audited bridge instead of hand-rolled assertion chains. A leaf module on
 * purpose — the suites' hoisted vi.mock factories call it while their own
 * imports are still evaluating.
 */
export function mockedSurface<S extends object, L extends object>(mock: L): S {
  // SAFETY: the literal covers exactly the surface the suite drives; this
  // bridge only relabels the object for the compiler, the runtime value is
  // unchanged.
  const wide = mock as object;

  // SAFETY: as above — the relabeled object carries the mocked members.
  return wide as S;
}
