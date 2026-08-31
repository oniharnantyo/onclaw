import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";

export interface IntegrationsSectionProps {
  tenant: any;
  onToast: (text: string, kind?: string) => void;
  onUpdate: (fn: any) => void;
}

export function IntegrationsSection({ tenant, onToast, onUpdate }: IntegrationsSectionProps) {
  return (
    <div className="max-w-lg space-y-3" data-od-id="pane-integrations" data-testid="pane-integrations">

      {(tenant.integrations || []).map((it: any) => (
        <div
          key={it.id}
          className="flex items-center gap-3 rounded-md border border-line bg-[color-mix(in_oklab,var(--bg)_30%,var(--surface))] px-4 py-3"
          data-od-id={'integration-' + it.id}
          data-testid={'integration-' + it.id}
        >
          <span
            className={cx(
              'flex h-9 w-9 items-center justify-center rounded-md',
              it.connected
                ? 'bg-[color-mix(in_oklab,var(--accent)_15%,transparent)] text-accent'
                : 'bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted'
            )}
          >
            <Icon
              name={
                it.id === 'slack'
                  ? 'chat'
                  : it.id === 'github'
                    ? 'terminal'
                    : it.id === 'pagerduty'
                      ? 'bell'
                      : it.id === 'postgres'
                        ? 'db'
                        : it.id === 'linear'
                          ? 'zap'
                          : 'file'
              }
              size={16}
            />
          </span>
          <div className="min-w-0 flex-1">
            <p className="text-[14px] font-medium text-fg">{it.name}</p>
            <p
              className={cx(
                'truncate font-mono text-[11px]',
                it.id === 'pagerduty' && it.connected
                  ? 'text-[color-mix(in_oklab,var(--warn),black_38%)]'
                  : 'text-muted'
              )}
            >
              {it.detail}
            </p>
          </div>
          {it.connected ? (
            <div className="flex items-center gap-2.5">
              <span className="flex items-center gap-1.5 text-[12px] text-[color-mix(in_oklab,var(--success),black_25%)]">
                <Icon name="check" size={13} /> Connected
              </span>
              <button
                type="button"
                data-od-id={'disconnect-' + it.id}
                data-testid={'disconnect-' + it.id}
                onClick={() => {
                  onUpdate((t: any) => ({
                    ...t,
                    integrations: (t.integrations || []).map((x: any) =>
                      x.id === it.id ? { ...x, connected: false } : x
                    ),
                  }));
                  onToast(it.name + ' disconnected');
                }}
                className="h-8 rounded-md px-2.5 text-[12px] font-medium text-muted transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] hover:text-fg2"
              >
                Disconnect
              </button>
            </div>
          ) : (
            <button
              type="button"
              data-od-id={'connect-' + it.id}
              data-testid={'connect-' + it.id}
              onClick={() => {
                onUpdate((t: any) => ({
                  ...t,
                  integrations: (t.integrations || []).map((x: any) =>
                    x.id === it.id ? { ...x, connected: true, detail: x.detail + ' · connected just now' } : x
                  ),
                }));
                onToast(it.name + ' connected');
              }}
              className="h-8 rounded-md border border-line px-3 text-[12px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg"
            >
              Connect
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
