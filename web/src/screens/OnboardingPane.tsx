import { Icon } from "../components/ui/Icon";
import { ErrorState } from "../components/ErrorState";

export function OnboardingPane({ tenant, onDeploy, onSettings, canDeploy = true }: { tenant: Workspace, onDeploy: () => void, onSettings: () => void, canDeploy?: boolean }) {
  return (
    <section
      data-od-id="onboarding-pane"
      data-testid="onboarding-pane"
      aria-label="Getting started"
      className="flex min-w-0 flex-1 flex-col items-center justify-center bg-surface px-6 text-center"
    >
      <ErrorState
        variant="compact"
        icon="bot"
        iconClassName="h-14 w-14 rounded-[16px] bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-accent"
        title={`${tenant.name} is ready`}
        description={
          canDeploy
            ? "A workspace is an API-backed container until it has agents. Deploy the first one — pick a model, grant tools, and it appears in the sidebar ready to chat."
            : "No agents here yet. An Owner or Admin can deploy the first one — after that it appears in the sidebar ready to chat."
        }
        primaryAction={
          canDeploy
            ? {
                label: (
                  <span className="flex items-center gap-2">
                    <Icon name="plus" size={15} sw={2.2} /> Deploy your first agent
                  </span>
                ),
                onClick: onDeploy,
                'data-testid': 'btn-onboarding-deploy',
              }
            : undefined
        }
        secondaryAction={{
          label: 'Workspace settings',
          onClick: onSettings,
          'data-testid': 'btn-onboarding-settings',
        }}
      >
        <p className="mt-2 font-mono text-[11px] text-muted">{tenant.tz}</p>
      </ErrorState>
    </section>
  );
}
