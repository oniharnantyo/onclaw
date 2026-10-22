/**
 * Toasts: bottom-right toast stack (onclaw-v2 parity, design D3). onclaw
 * passes the array down from its app store; buddies' toasts live in the
 * lib/toasts module store, so this subscribes directly and takes no props.
 * kind "danger" shows the alert glyph in text-danger; every other kind shows
 * the check glyph in text-accent.
 */
import { useSyncExternalStore } from "react";
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";
import { getToasts, subscribeToasts, type Toast } from "../../lib/toasts";

import { Icon } from "./Icon";

export function Toasts(): ReactElement {
  const toasts = useSyncExternalStore(subscribeToasts, getToasts);

  return (
    <div
      className="pointer-events-none fixed bottom-5 right-5 z-[70] flex w-80 flex-col gap-2"
      role="status"
      aria-live="polite"
    >
      {toasts.map((t: Toast): ReactElement => (
        <div
          key={t.id}
          className="od-pop pointer-events-auto flex items-start gap-2.5 rounded-md border border-line bg-warm px-3.5 py-2.5 shadow-[var(--elev-raised)]"
        >
          <span
            className={cn(
              "mt-0.5",
              t.kind === "danger" ? "text-danger" : "text-accent"
            )}
          >
            <Icon name={t.kind === "danger" ? "alert" : "check"} size={15} />
          </span>
          <p className="text-[13px] leading-5 text-fg2">{t.text}</p>
        </div>
      ))}
    </div>
  );
}
