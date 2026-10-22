/**
 * App-shell wiring for the native menu's Settings… item (design D5, task
 * 5.2): the untyped `settings:open` signal flips the nav store to settings.
 * Extracted from App.tsx so the subscription is unit-testable without
 * rendering the shell and its three views.
 */
import type { Unsubscribe } from "./lib/transport";
import { client } from "./lib/client";
import { useNavStore } from "./stores/nav";

/**
 * Subscribes to the native menu's settings signal; the returned function
 * unsubscribes (the shell calls it on unmount).
 */
export function subscribeSettingsShortcut(): Unsubscribe {
  return client.events.onSettingsOpen((): void =>
    useNavStore.getState().setMenu("settings")
  );
}
