/**
 * ThemeCycleButton: the three-state theme cycle control (onclaw-v2 parity,
 * tasks 2.1/2.5). The icon mirrors the CURRENT preference (sun/moon/monitor —
 * monitor for system regardless of which scheme the OS currently renders),
 * and the accessible name + tooltip carry the exact contract text
 * `Theme: <current> (click for <next>)` — visible label and aria-label always
 * describe the same thing (design D9 fixes the old label/aria mismatch).
 *
 * Rewired for buddies: onclaw drove lib/theme directly from local state;
 * here the zustand store (stores/theme.ts) owns the preference and applies
 * it through the same lib/theme engine.
 */
import type { ReactElement } from "react";

import { cn } from "../../lib/cn";
import { cyclePreference, type ThemePreference } from "../../lib/theme";
import { useThemeStore } from "../../stores/theme";

import { Icon, type IconName } from "./Icon";
import { Tooltip } from "./Tooltip";

const ICON_FOR: Record<ThemePreference, IconName> = {
  light: "sun",
  dark: "moon",
  system: "monitor",
};

const contractText = (pref: ThemePreference): string =>
  `Theme: ${pref} (click for ${cyclePreference(pref)})`;

export function ThemeCycleButton({
  variant,
  expanded = false,
  testId = "theme-cycle",
  odId,
}: {
  variant: "rail" | "icon";
  expanded?: boolean;
  testId?: string;
  odId?: string;
}): ReactElement {
  const preference = useThemeStore(
    (s: ReturnType<typeof useThemeStore.getState>) => s.preference
  );

  const setPreference = useThemeStore(
    (s: ReturnType<typeof useThemeStore.getState>) => s.setPreference
  );

  const text = contractText(preference);

  const cycle = (): void => {
    setPreference(cyclePreference(preference));
  };

  // Rail-expanded renders the full labeled row, so the tooltip would duplicate
  // visible text — disabled there, exactly like the rail's own rows.
  const showLabel = variant === "rail" && expanded;

  return (
    <Tooltip
      content={text}
      placement={variant === "rail" ? "right" : "bottom"}
      disabled={showLabel}
    >
      <button
        type="button"
        onClick={cycle}
        data-testid={testId}
        data-od-id={odId ?? testId}
        aria-label={text}
        className={cn(
          "flex items-center rounded-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)]",
          showLabel
            ? "h-[30px] w-[calc(100%-16px)] mx-2 gap-2 px-3 text-left hover:text-fg2"
            : variant === "rail"
              ? "h-11 w-11 justify-center hover:text-fg2"
              : "h-9 w-9 justify-center rounded-md hover:text-fg"
        )}
      >
        <Icon
          name={ICON_FOR[preference]}
          size={variant === "rail" ? 19 : 17}
          sw={1.7}
          className="shrink-0"
        />
        {showLabel && (
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
            Theme: {preference}
          </span>
        )}
      </button>
    </Tooltip>
  );
}
