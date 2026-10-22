import { describe, expect, it, vi } from "vite-plus/test";

import { createConnectionStore } from "../connection";

describe("connection store", () => {
  it("starts connected and stays connected on a successful probe", async () => {
    const probe = vi.fn(async () => ({ name: "buddies", version: "0" }));
    const useStore = createConnectionStore(probe);

    expect(useStore.getState().status).toBe("connected");
    const status = await useStore.getState().probe();
    expect(status).toBe("connected");
    expect(useStore.getState().status).toBe("connected");
  });

  it("goes offline when the probe rejects and recovers afterwards", async () => {
    let fail = true;

    const probe = vi.fn(async () => {
      if (fail) throw new Error("unreachable");

      return {};
    });

    const useStore = createConnectionStore(probe);

    await useStore.getState().probe();
    expect(useStore.getState().status).toBe("offline");

    fail = false;
    const status = await useStore.getState().probe();
    expect(status).toBe("connected");
    expect(useStore.getState().status).toBe("connected");
  });

  it("notifies subscribers of status transitions", async () => {
    const probe = vi.fn(async () => {
      throw new Error("down");
    });

    const useStore = createConnectionStore(probe);
    const seen: string[] = [];

    const unsub = useStore.subscribe((state: { status: string }) =>
      seen.push(state.status)
    );

    await useStore.getState().probe();
    expect(seen).toEqual(["offline"]);
    unsub();
  });
});
