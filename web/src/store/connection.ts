import { create } from 'zustand';

export interface ConnectionState {
  degraded: boolean;
  setDegraded: (degraded: boolean) => void;
  reportSuccess: () => void;
  reportFailure: () => void;
  reset: () => void;
}

export const useConnectionStore = create<ConnectionState>((set) => ({
  degraded: false,
  setDegraded: (degraded: boolean) => set({ degraded }),
  reportSuccess: () => set({ degraded: false }),
  reportFailure: () => set({ degraded: true }),
  reset: () => set({ degraded: false }),
}));
