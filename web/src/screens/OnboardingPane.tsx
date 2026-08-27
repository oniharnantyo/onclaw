import { Icon } from "../components/ui/Icon";

export function OnboardingPane({ tenant, onDeploy, onSettings }: { tenant: Workspace, onDeploy: () => void, onSettings: () => void }) {
  return (
    <section data-od-id="onboarding-pane" aria-label="Getting started"
      className="flex min-w-0 flex-1 flex-col items-center justify-center gap-4 bg-surface px-6 text-center">
      <div className="flex h-14 w-14 items-center justify-center rounded-[16px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-accent">
        <Icon name="bot" size={26}/>
      </div>
      <div>
        <h1 className="text-[24px] font-semibold tracking-[-0.01em] text-fg">{tenant.name} is ready</h1>
        <p className="mx-auto mt-2 max-w-md text-[14px] leading-6 text-muted">A workspace is a container until it has agents. Deploy the first one — pick a model, grant tools, and it appears in the sidebar ready to chat.</p>
      </div>
      <div className="mt-1 flex items-center gap-2.5">
        <button type="button" onClick={onDeploy} data-od-id="btn-onboarding-deploy"
          className="flex h-9 items-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)]">
          <Icon name="plus" size={15} sw={2.2}/> Deploy your first agent
        </button>
        <button type="button" onClick={onSettings} data-od-id="btn-onboarding-settings"
          className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:border-accent hover:text-fg">
          Workspace settings
        </button>
      </div>
      <p className="mt-2 font-mono text-[11px] text-muted">{tenant.plan} plan · {tenant.tz}</p>
    </section>
  );
}

