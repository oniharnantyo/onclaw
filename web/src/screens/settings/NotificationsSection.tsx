import { useState } from "react";
import { cx } from "../../lib/helpers";
import { Toggle } from "../../components/ui/Toggle";
import { inputCls, labelCls } from "../../components/ui/constants";

export interface NotificationsSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate: (fn: any) => void;
}

export function NotificationsSection({ tenant, onToast, onUpdate }: NotificationsSectionProps) {
  const [notif, setNotif] = useState(
    tenant.notifications || {
      cronFail: true,
      agentErrors: true,
      digest: false,
      email: 'ops@' + tenant.sub + '.dev',
    }
  );

  return (
    <div className="max-w-md space-y-1" data-od-id="pane-notifications" data-testid="pane-notifications">
      {[
        { id: 'cronFail', label: 'Cron failures', desc: 'A scheduled run fails or misses its window' },
        { id: 'agentErrors', label: 'Agent errors', desc: 'Tool failures, auth expiry, budget thresholds' },
        { id: 'digest', label: 'Weekly digest', desc: 'Monday summary of runs, spend and failures' },
      ].map((row) => (
        <div key={row.id} className="flex items-center justify-between gap-4 rounded-md px-1 py-3">
          <div>
            <p className="text-[14px] font-medium text-fg">{row.label}</p>
            <p className="text-[12px] text-muted">{row.desc}</p>
          </div>
          <Toggle
            on={notif[row.id]}
            onChange={(v) => setNotif({ ...notif, [row.id]: v })}
            label={row.label}
          />
        </div>
      ))}
      <div className="pt-2">
        <label className={labelCls} htmlFor="nf-email">
          Route email
        </label>
        <input
          id="nf-email"
          className={cx(inputCls, 'font-mono text-[13px]')}
          value={notif.email}
          onChange={(e) => setNotif({ ...notif, email: e.target.value })}
        />
      </div>
      <div className="flex justify-end pt-3">
        <button
          type="button"
          data-od-id="btn-notif-save"
          data-testid="btn-notif-save"
          onClick={() => {
            onUpdate((t: any) => ({ ...t, notifications: notif }));
            onToast('Notification settings saved');
          }}
          className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]"
        >
          Save preferences
        </button>
      </div>
    </div>
  );
}
