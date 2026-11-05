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
          "flex items-center transition-colors",
          showLabel
            ? // SideRow parity (rail footer alignment): identical row
              // geometry, icon column, and idle/hover colors as the
              // Settings row rendered directly below it.
              "h-[30px] w-full gap-2 rounded-md px-2.5 text-left text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
            : variant === "rail"
              ? "h-11 w-11 justify-center rounded-[12px] text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
              : "h-9 w-9 justify-center rounded-md text-muted hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg"
        )}
      >
        <Icon
          name={ICON_FOR[preference]}
          size={showLabel ? 16 : variant === "rail" ? 19 : 17}
          sw={1.7}
          className="shrink-0"
        />
        {showLabel && (
          <span className="min-w-0 flex-1 truncate text-[13px]">
            Theme: {preference}
          </span>
        )}
      </button>
    </Tooltip>
  );
}
