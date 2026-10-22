/**
 * Connection status for server mode (design D1/D7): the app is either
 * reachable through the Wails bridge (webview or WebSocket) or not. Desktop
 * webview mode is always connected; in server mode UI can show the offline
 * state after a failed probe and recover when a later probe succeeds.
 */
import { create, type StoreApi, type UseBoundStore } from "zustand";

import { client, type ProbeTarget } from "../lib/client";

export type ConnectionStatus = "connected" | "offline";

/** Any backend call that doubles as a liveness probe. */
export type ProbeFn = () => Promise<ProbeTarget>;

interface ConnectionStore {
  status: ConnectionStatus;
  /** Re-probes the backend and updates the status from the outcome. */
  probe: () => Promise<ConnectionStatus>;
}

export type ConnectionStoreApi = UseBoundStore<StoreApi<ConnectionStore>>;

type ConnectionSetState = StoreApi<ConnectionStore>["setState"];

type ConnectionGetState = StoreApi<ConnectionStore>["getState"];

export function createConnectionStore(probe: ProbeFn): ConnectionStoreApi {
  const useStore = create<ConnectionStore>(
    (set: ConnectionSetState, get: ConnectionGetState) => ({
      status: "connected",
      probe: async (): Promise<ConnectionStatus> => {
        try {
          await probe();
          set({ status: "connected" });
        } catch {
          set({ status: "offline" });
        }

        return get().status;
      },
    })
  );

  return useStore;
}

function defaultProbe(): Promise<ProbeTarget> {
  return client.appInfo.get();
}

/** Application-wide connection store probing via the app info service. */
export const useConnectionStore = createConnectionStore(defaultProbe);
