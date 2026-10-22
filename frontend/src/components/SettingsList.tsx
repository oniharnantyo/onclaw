import type { ReactElement } from "react";

import type { SettingsListProps, SettingsRowProps } from "./types";

/**
 * Row container for the settings panes. onclaw-v2's list grammar — a
 * hairline-bordered surface with soft divided rows — gives every pane's
 * table one silhouette; callers supply row content via `SettingsRow`.
 */
export function SettingsList({
  children,
  testId,
}: SettingsListProps): ReactElement {
  return (
    <ul
      className="divide-y divide-linesoft rounded-md border border-line"
      data-testid={testId}
    >
      {children}
    </ul>
  );
}

/**
 * One settings row: an optional leading brand tile, then a label column
 * (title + badges + detail + any extra block) that grows, and a trailing
 * action cluster pinned to the row's right edge and vertically centered on
 * the label column. The panes repeat this exactly — only the badges and
 * actions differ.
 */
export function SettingsRow({
  lead,
  title,
  badges,
  detail,
  extra,
  actions,
  testId,
}: SettingsRowProps): ReactElement {
  return (
    <li className="flex items-center gap-3 px-4 py-3" data-testid={testId}>
      <div className="min-w-0 flex-1">
        <div
          className={`flex flex-wrap items-center ${lead ? "gap-3" : "gap-2"}`}
        >
          {lead}
          <span className="font-medium text-fg">{title}</span>
          {badges}
        </div>
        {detail && <div className="truncate text-xs text-muted">{detail}</div>}
        {extra}
      </div>
      {actions && (
        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      )}
    </li>
  );
}
