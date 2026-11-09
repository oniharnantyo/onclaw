import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { NotificationSettings } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { LoadingHint, SettingsList, SettingsRow } from "../../../components";
import { Alert } from "../../../components/ui/Alert";
import { Toggle } from "../../../components/ui/Toggle";
import { client } from "../../../lib/client";
import type { SettingsPaneConfig } from "../types";

/**
 * The toggleable flags of the settings singleton: the master switch plus the
 * four categories — every one of them a boolean field of the generated row.
 */
type NotificationFlag =
  | "Enabled"
  | "Turns"
  | "Attention"
  | "Schedules"
  | "Groups";

/**
 * The four notification categories in pane order (add-desktop-notifications
 * spec): the flags' names ride the generated NotificationSettings fields.
 */
const CATEGORIES: {
  field: Exclude<NotificationFlag, "Enabled">;
  label: string;
  detail: string;
}[] = [
  {
    field: "Turns",
    label: "Agent turns",
    detail: "Notify me when an agent finishes a turn.",
  },
  {
    field: "Attention",
    label: "Needs attention",
    detail:
      "Notify me when an agent parks waiting on my approval or input.",
  },
  {
    field: "Schedules",
    label: "Schedules",
    detail: "Notify me when a scheduled run completes or misses.",
  },
  {
    field: "Groups",
    label: "Groups",
    detail: "Notify me when a group task reaches a terminal state.",
  },
];

/**
 * NotificationsPane: the desktop-notification preferences (add-desktop-
 * notifications tasks 6.1, design D7, spec settings "Notifications pane").
 * Loads the persisted singleton on open, saves durably on every toggle
 * (the full settings object; the save path refreshes the engine's cache, so
 * changes apply live), and reflects the saved state. The master toggle gates
 * the categories: master-off renders the category rows disabled while their
 * stored values ride along untouched — re-enabling the master restores them.
 * Kit components and tokens only, so both themes re-skin the pane.
 */
export default function NotificationsPane(_props: {
  pane: SettingsPaneConfig;
}): ReactElement {
  const [settings, setSettings] = useState<NotificationSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  const load = async (): Promise<void> => {
    setLoading(true);
    setLoadError(null);

    try {
      setSettings(await client.notifications.getNotificationSettings());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  /**
   * One toggle flip: patches the named flag and persists the whole settings
   * object; the state updates only after the save resolves, so the pane
   * always reflects the durable state.
   */
  const toggle = async (field: NotificationFlag, value: boolean): Promise<void> => {
    if (!settings) return;

    setSaveError(null);

    // SAFETY: `field` is always one of the row's own boolean flags, so the
    // spread carries every required field and the patch flips exactly one.
    const next = { ...settings, [field]: value } as NotificationSettings;

    try {
      await client.notifications.saveNotificationSettings(next);
      setSettings(next);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    }
  };

  const masterOn = settings?.Enabled ?? false;

  return (
    <div data-testid="notifications-pane">
      {loading ? (
        <LoadingHint testId="notifications-loading" />
      ) : loadError ? (
        <div role="alert">
          <Alert tone="danger" testId="notifications-error">
            {loadError}
          </Alert>
        </div>
      ) : (
        <SettingsList testId="notifications-list">
          <SettingsRow
            title="Desktop notifications"
            testId="notifications-master"
            detail="The master switch — turn off to silence every category."
            actions={
              <Toggle
                on={masterOn}
                testId="notifications-toggle-master"
                label="Enable desktop notifications"
                onChange={(next: boolean) => void toggle("Enabled", next)}
              />
            }
          />
          {CATEGORIES.map(
            (category: {
              field: Exclude<NotificationFlag, "Enabled">;
              label: string;
              detail: string;
            }) => (
              <SettingsRow
                key={category.field}
                title={category.label}
                testId={`notifications-category-${category.field.toLowerCase()}`}
                detail={category.detail}
                actions={
                  <Toggle
                    on={settings?.[category.field] ?? false}
                    disabled={!masterOn}
                    testId={`notifications-toggle-${category.field.toLowerCase()}`}
                    label={`Enable ${category.label} notifications`}
                    onChange={(next: boolean) =>
                      void toggle(category.field, next)
                    }
                  />
                }
              />
            )
          )}
        </SettingsList>
      )}

      {saveError && (
        <div role="alert" className="mt-2">
          <Alert tone="danger" testId="notifications-save-error">
            {saveError}
          </Alert>
        </div>
      )}
    </div>
  );
}
