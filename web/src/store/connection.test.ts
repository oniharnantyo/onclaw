import { describe, it, expect, beforeEach } from 'vitest';
import { useConnectionStore } from './connection';

describe('store/connection', () => {
  beforeEach(() => {
    useConnectionStore.getState().reset();
  });

  it('initializes with degraded: false', () => {
    expect(useConnectionStore.getState().degraded).toBe(false);
  });

  it('sets degraded to true on reportFailure()', () => {
    useConnectionStore.getState().reportFailure();
    expect(useConnectionStore.getState().degraded).toBe(true);
  });

  it('resets degraded to false on reportSuccess()', () => {
    useConnectionStore.getState().reportFailure();
    expect(useConnectionStore.getState().degraded).toBe(true);

    useConnectionStore.getState().reportSuccess();
    expect(useConnectionStore.getState().degraded).toBe(false);
  });

  it('sets degraded via setDegraded(boolean)', () => {
    useConnectionStore.getState().setDegraded(true);
    expect(useConnectionStore.getState().degraded).toBe(true);

    useConnectionStore.getState().setDegraded(false);
    expect(useConnectionStore.getState().degraded).toBe(false);
  });

  it('resets state via reset()', () => {
    useConnectionStore.getState().setDegraded(true);
    expect(useConnectionStore.getState().degraded).toBe(true);

    useConnectionStore.getState().reset();
    expect(useConnectionStore.getState().degraded).toBe(false);
  });
});
