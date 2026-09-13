import React, { useState } from 'react';
import { cx } from '../../lib/helpers';
import { Icon } from './Icon';
import { Tooltip } from './Tooltip';
import {
  applyTheme,
  cyclePreference,
  getStoredPreference,
  setStoredPreference,
  type ThemePreference,
} from '../../lib/theme';

// Theme preference cycle control (change add-dark-theme, task 3): one shared
// button for both homes — the rail's bottom cluster and the login screen's
// corner. The icon mirrors the CURRENT preference (sun/moon/monitor — monitor
// for system regardless of which scheme the OS currently renders), and the
// accessible name + tooltip carry the exact contract text
// `Theme: <current> (click for <next>)`.
const ICON_FOR: Record<ThemePreference, string> = {
  light: 'sun',
  dark: 'moon',
  system: 'monitor',
};

const contractText = (pref: ThemePreference) =>
  `Theme: ${pref} (click for ${cyclePreference(pref)})`;

export function ThemeCycleButton({
  variant,
  expanded = false,
  testId = 'theme-cycle',
  odId,
}: {
  variant: 'rail' | 'icon';
  expanded?: boolean;
  testId?: string;
  odId?: string;
}) {
  const [pref, setPref] = useState<ThemePreference>(() => getStoredPreference());
  const text = contractText(pref);

  const cycle = () => {
    const next = cyclePreference(pref);
    setStoredPreference(next);
    applyTheme(next);
    setPref(next);
  };

  // Rail-expanded renders the full labeled row, so the tooltip would duplicate
  // visible text — disabled there, exactly like the rail's own rows.
  const showLabel = variant === 'rail' && expanded;

  return (
    <Tooltip
      content={text}
      placement={variant === 'rail' ? 'right' : 'bottom'}
      disabled={showLabel}
    >
      <button
        type="button"
        onClick={cycle}
        data-testid={testId}
        data-od-id={odId ?? testId}
        aria-label={text}
        className={cx(
          'flex items-center rounded-[12px] text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)]',
          showLabel
            ? 'h-11 w-[calc(100%-16px)] mx-2 gap-3 px-3 text-left hover:text-fg2'
            : variant === 'rail'
              ? 'h-11 w-11 justify-center hover:text-fg2'
              : 'h-9 w-9 justify-center rounded-md hover:text-fg'
        )}
      >
        <Icon
          name={ICON_FOR[pref]}
          size={variant === 'rail' ? 19 : 17}
          sw={1.7}
          className="shrink-0"
        />
        {showLabel && (
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium">
            Theme: {pref}
          </span>
        )}
      </button>
    </Tooltip>
  );
}
