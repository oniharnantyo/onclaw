import type { ReactElement } from "react";

import type { PaneHeaderProps } from "./types";

/**
 * Pane header row (redesign-shell-layout D5): the pane title on the left and
 * an optional action cluster on the right, on one row; an optional
 * description renders as a muted line below. Settings panes open with it so
 * every pane's header behaves identically.
 */
export function PaneHeader({
  title,
  description,
  actions,
  testId = "settings-pane-header",
}: PaneHeaderProps): ReactElement {
  return (
    <div data-testid={testId}>
      {title && (
        <div className="flex items-center">
          <h1
            className="font-display text-lg font-semibold"
            data-testid="settings-pane-title"
          >
            {title}
          </h1>
          {actions && (
            <div className="ml-auto flex items-center gap-2">{actions}</div>
          )}
        </div>
      )}
      {!title && actions && (
        <div className="flex items-center">
          <div className="ml-auto flex items-center gap-2">{actions}</div>
        </div>
      )}
      {description && (
        <p
          className="mt-1 text-sm text-muted"
          data-testid="settings-pane-description"
        >
          {description}
        </p>
      )}
    </div>
  );
}
