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
import { KeysSection } from "./KeysSection";
import { NotificationsSection } from "./NotificationsSection";
import { StoragePane } from "./StoragePane";

export const SETTINGS_SECTIONS = [
  { id: 'workspace', label: 'Workspace', icon: 'shield' },
  { id: 'providers', label: 'Providers', icon: 'sliders' },
  { id: 'members', label: 'Members & roles', icon: 'users' },
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
      className="flex h-full flex-1 flex-col md:flex-row overflow-hidden bg-surface"
    >
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
  );
}
