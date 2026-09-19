import { useParams, useNavigate, Navigate } from "react-router-dom";
import { cx } from "../../lib/helpers";
import { Icon } from "../../components/ui/Icon";
import { useStore, useWorkspace } from "../../store";

import { WorkspaceSection } from "./WorkspaceSection";
import { ProvidersPane } from "./ProvidersPane";
import { MembersSection } from "./MembersSection";
import { IntegrationsSection } from "./IntegrationsSection";
import { GatewaysPane } from "./GatewaysPane";
import { McpPane } from "./McpPane";
import { SkillsPane } from "./SkillsPane";
import { ToolsPane } from "./ToolsPane";
import { HooksPane } from "./HooksPane";
import { MemoryPane } from "./MemoryPane";
import { KeysSection } from "./KeysSection";
import { NotificationsSection } from "./NotificationsSection";
import { StoragePane } from "./StoragePane";

export const SETTINGS_SECTIONS = [
  { id: 'workspace', label: 'Workspace', icon: 'shield' },
  { id: 'providers', label: 'Providers', icon: 'sliders' },
  { id: 'members', label: 'Members & roles', icon: 'users' },
  { id: 'memory', label: 'Memory', icon: 'memory' },
  { id: 'gateways', label: 'Gateways', icon: 'bot' },
  { id: 'integrations', label: 'Integrations', icon: 'link' },
  { id: 'mcp', label: 'MCP servers', icon: 'plug' },
  { id: 'skills', label: 'Skills', icon: 'spark' },
  { id: 'tools', label: 'Tools', icon: 'zap' },
  { id: 'hooks', label: 'Hooks', icon: 'activity' },
  { id: 'keys', label: 'API keys', icon: 'key' },
  { id: 'notifications', label: 'Notifications', icon: 'bell' },
  { id: 'storage', label: 'Storage', icon: 'db' },
] as const;

export type SettingsSectionId = (typeof SETTINGS_SECTIONS)[number]['id'];

const VALID_SECTIONS = new Set<string>(SETTINGS_SECTIONS.map((s) => s.id));

const LAST_NON_SETTINGS_PATH_KEY = 'onclaw:lastNonSettingsPath';

/**
 * Remember the route to return to from settings. The layout calls this on every
 * pathname change; settings paths are skipped so the most recent non-settings
 * route survives navigating between sections. sessionStorage keeps the origin
 * across reloads in the current tab.
 */
export function rememberLastNonSettingsPath(pathname: string): void {
  if (pathname.startsWith('/settings')) return;
  try {
    sessionStorage.setItem(LAST_NON_SETTINGS_PATH_KEY, pathname);
  } catch {
    // sessionStorage unavailable (e.g. storage disabled) — Back falls back to /c.
  }
}

/** The route the user came from, or `/c` for deep links with no in-app history. */
export function getLastNonSettingsPath(): string {
  try {
    return sessionStorage.getItem(LAST_NON_SETTINGS_PATH_KEY) || '/c';
  } catch {
    return '/c';
  }
}

export interface SettingsPageProps {
  tenant?: any;
  onToast?: (text: string, kind?: string) => void;
  onUpdate?: (fn: any) => void;
  onLeaveWorkspace?: () => Promise<void> | void;
  /** Override the derived skills.write check for the Skills pane (tests). */
  skillsCanWrite?: boolean;
}

export function SettingsPage({
  tenant: propTenant,
  onToast: propOnToast,
  onUpdate: propOnUpdate,
  onLeaveWorkspace,
  skillsCanWrite,
}: SettingsPageProps = {}) {
  const { section = 'workspace' } = useParams<{ section?: string }>();
  const navigate = useNavigate();
  const currentWorkspace = useWorkspace();
  const toastFromStore = useStore((s: any) => s.toast);

  const tenant = propTenant || currentWorkspace;
  const onToast = propOnToast || toastFromStore;
  const onUpdate =
    propOnUpdate ||
    ((fn: any) => useStore.getState().updateTenant(tenant?.id, fn));

  if (!VALID_SECTIONS.has(section)) {
    return <Navigate to="/settings/workspace" replace />;
  }

  return (
    <div
      data-od-id="settings-page"
      data-testid="settings-page"
      className="flex h-full flex-1 flex-col overflow-hidden bg-surface"
    >
      {/* Header row: explicit way back to the route the user came from.
          Rendered at every viewport width. */}
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-linesoft px-2 md:px-4">
        <button
          type="button"
          data-od-id="settings-back"
          data-testid="settings-back"
          aria-label="Back"
          onClick={() => navigate(getLastNonSettingsPath())}
          className="flex h-8 items-center gap-1.5 rounded-md px-2 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg focus-visible:border-accent focus-visible:outline-none"
        >
          <Icon name="arrow-left" size={15} className="text-muted" />
          <span>Back</span>
        </button>
        <h1 className="text-[13px] font-semibold text-fg">Settings</h1>
      </header>

      <div className="flex min-h-0 flex-1 flex-col md:flex-row overflow-hidden">
      {/* Section Nav: horizontal scroll tabs below md (<768px), static left column on md+ (>=768px) */}
      <nav
        className="od-scroll w-full md:w-56 shrink-0 flex md:flex-col overflow-x-auto md:overflow-x-hidden md:overflow-y-auto border-b md:border-b-0 md:border-r border-linesoft py-3 px-2 md:px-0"
        role="tablist"
        aria-label="Settings sections"
      >
        {SETTINGS_SECTIONS.map((t) => {
          const active = section === t.id;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => navigate(`/settings/${t.id}`)}
              data-od-id={'settings-tab-' + t.id}
              data-testid={'settings-tab-' + t.id}
              className={cx(
                'mx-1 md:mx-2 flex h-9 shrink-0 md:shrink items-center gap-2.5 rounded-md px-3 text-left text-[13px] transition-colors',
                active
                  ? 'bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] font-medium text-fg'
                  : 'text-fg2 hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg'
              )}
            >
              <Icon
                name={t.icon}
                size={15}
                className={active ? 'text-accent' : 'text-muted'}
              />
              <span className="whitespace-nowrap">{t.label}</span>
            </button>
          );
        })}
      </nav>

      {/* Content Area */}
      <div className="od-scroll flex-1 overflow-y-auto p-4 md:p-6 lg:p-8">
        <div className="mx-auto max-w-5xl">
          <div className="mb-6">
            <h2 className="text-xl font-semibold text-fg">
              {SETTINGS_SECTIONS.find((s) => s.id === section)?.label}
            </h2>
          </div>

          {section === 'workspace' && (
            <WorkspaceSection
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
              onLeaveWorkspace={onLeaveWorkspace}
            />
          )}

          {section === 'providers' && (
            <ProvidersPane
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'members' && (
            <MembersSection
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'integrations' && (
            <IntegrationsSection
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'memory' && (
            <MemoryPane
              tenant={tenant}
              onUpdate={onUpdate}
              onToast={onToast}
            />
          )}

          {section === 'gateways' && (
            <GatewaysPane
              tenant={tenant}
              onUpdate={onUpdate}
              onToast={onToast}
            />
          )}

          {section === 'mcp' && (
            <McpPane
              tenant={tenant}
              onUpdate={onUpdate}
              onToast={onToast}
            />
          )}

          {section === 'skills' && (
            <SkillsPane
              tenant={tenant}
              onUpdate={onUpdate}
              onToast={onToast}
              canWrite={skillsCanWrite}
            />
          )}

          {section === 'tools' && (
            <ToolsPane
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'hooks' && (
            <HooksPane
              tenant={tenant}
              onUpdate={onUpdate}
              onToast={onToast}
            />
          )}

          {section === 'keys' && (
            <KeysSection
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'notifications' && (
            <NotificationsSection
              tenant={tenant}
              onToast={onToast}
              onUpdate={onUpdate}
            />
          )}

          {section === 'storage' && (
            <StoragePane tenant={tenant} onToast={onToast} />
          )}
        </div>
      </div>
      </div>
    </div>
  );
}
