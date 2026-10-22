/**
 * Alert: the kit's inline banner (design D3). onclaw-v2 inline-styles these —
 * a flat tone-tinted surface with a hairline tone border and a leading icon
 * (`border-[…tone…35%] bg-[…tone…8%]`, `Icon name="alert"` at the text top) —
 * so buddies lifts that exact idiom into a component. Danger text rides the
 * lifted `--danger-text` tier so it clears contrast in both themes (D9); the
 * warn/success/info bodies stay fg2 with the tint + icon carrying the tone.
 *
 * Not a live region: static banners, matching onclaw (callers add role=
 * "alert" per-instance where content appears asynchronously).
 */
import type { ReactElement, ReactNode } from "react";

import { cn } from "../../lib/cn";
import { Icon } from "./Icon";
import type { IconName } from "./Icon";

export type AlertTone = "info" | "success" | "warn" | "danger";

export type AlertProps = {
  tone?: AlertTone;
  children: ReactNode;
  /** Extra classes on the banner (margins, width overrides). */
  className?: string;
  testId?: string;
};

/** onclaw-v2 alert idiom per tone: hairline tint border + tinted bg. */
const TONE_CLASSES: Record<AlertTone, string> = {
  info: "border-[color-mix(in_oklab,var(--accent)_35%,transparent)] bg-[color-mix(in_oklab,var(--accent)_8%,transparent)] text-fg2",
  success:
    "border-[color-mix(in_oklab,var(--success)_35%,transparent)] bg-[color-mix(in_oklab,var(--success)_8%,transparent)] text-fg2",
  warn: "border-[color-mix(in_oklab,var(--warn)_35%,transparent)] bg-[color-mix(in_oklab,var(--warn)_8%,transparent)] text-fg2",
  danger:
    "border-[color-mix(in_oklab,var(--danger)_35%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] text-dangertext",
};

const TONE_ICONS: Record<AlertTone, { name: IconName; className: string }> = {
  info: { name: "bell", className: "text-accent" },
  success: { name: "check-circle", className: "text-success" },
  warn: {
    name: "alert",
    className: "text-[color-mix(in_oklab,var(--warn),black_25%)]",
  },
  danger: { name: "alert", className: "text-dangertext" },
};

export function Alert({
  tone = "info",
  children,
  className,
  testId,
}: AlertProps): ReactElement {
  const icon = TONE_ICONS[tone];

  return (
    <div
      data-testid={testId}
      className={cn(
        "flex items-start gap-1.5 rounded-md border px-3 py-2.5 text-[13px] leading-relaxed",
        TONE_CLASSES[tone],
        className
      )}
    >
      <Icon
        name={icon.name}
        size={14}
        className={cn("mt-0.5 shrink-0", icon.className)}
      />
      <div className="min-w-0">{children}</div>
    </div>
  );
}
