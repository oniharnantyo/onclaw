import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useNavigate, useLocation } from 'react-router-dom';
import { useStore, useWorkspace, useSearchShortcut, useThread } from './store';
import { useAuthStore, useAuth, useIsAdmin } from './store/auth';
import { Rail } from './components/nav/Rail';
import { Sidebar } from './components/nav/Sidebar';
import { WorkspaceSwitcher } from './components/nav/WorkspaceSwitcher';
import { NavDrawer } from './components/nav/NavDrawer';
import { Toasts } from './components/ui/Toasts';
import { AgentsView } from './screens/AgentsView';
import { SchedulesView } from './screens/SchedulesView';
import { RunsView } from './screens/RunsView';
import { OnboardingPane } from './screens/OnboardingPane';
import { LoginView } from './screens/LoginView';
import { AdminView } from './screens/admin/AdminView';
import { SettingsPage } from './screens/settings';
import { ChatRoute } from './screens/ChatRoute';
import { AgentConfigModal } from './modals/AgentConfigModal';
import { ScheduleEditorModal } from './modals/ScheduleEditorModal';
import { CreateWorkspaceModal } from './modals/CreateWorkspaceModal';
import { BootError } from './components/BootError';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ErrorState } from './components/ErrorState';
import { ConnectionBanner } from './components/ConnectionBanner';
import { api, formatApiError } from "./lib/api";
import notFoundSvg from './assets/not-found.svg';

function BootGate({ children }: { children: React.ReactNode }) {
  const status = useAuthStore((s) => s.status);
  const boot = useAuthStore((s) => s.boot);

  useEffect(() => {
    boot();
  }, [boot]);

  if (status === 'loading') {
    return (
      <div className="flex h-[100dvh] w-full items-center justify-center bg-bg">
        <div className="flex flex-col items-center gap-3">
          <div className="h-7 w-7 animate-spin rounded-full border-2 border-line border-t-accent" />
          <span className="text-[13px] text-muted font-medium">Loading OnClaw…</span>
        </div>
      </div>
    );
  }

  if (status === 'error') {
    return <BootError />;
  }

  return <>{children}</>;
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const status = useAuthStore((s) => s.status);
  const location = useLocation();

  if (status === 'unauthenticated') {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <>{children}</>;
}

function NotFoundRoute() {
  const navigate = useNavigate();
  return (
    <div className="flex h-full w-full items-center justify-center p-6 bg-surface">
      <ErrorState
        variant="full"
        illustration={notFoundSvg}
        title="Page not found"
        description="The page you are looking for does not exist or has moved."
        status={404}
        primaryAction={{
          label: 'Back to chats',
          onClick: () => navigate('/c'),
        }}
        secondaryAction={{
          label: 'View agents',
          onClick: () => navigate('/agents'),
        }}
      />
    </div>
  );
}

function Layout() {
  useSearchShortcut();
  const navigate = useNavigate();
  const location = useLocation();
  const tenant = useWorkspace();
  const { logout } = useAuth();
  const isAdmin = useIsAdmin();
  const memberships = useAuthStore((s) => s.memberships);

  const ui = useStore((s: any) => s.ui);
  const patchUi = useStore((s: any) => s.patchUi);
  const search = useStore((s: any) => s.search);
  const setSearch = useStore((s: any) => s.setSearch);
  const unread = (tenant?.channels || []).reduce((n, c) => n + (c.unread || 0), 0);

  const [drawerOpen, setDrawerOpen] = useState(false);
  const [createWsOpen, setCreateWsOpen] = useState(false);

  const handleLogout = async () => {
    await logout();
    navigate('/login', { replace: true });
  };

  // Map route to view string for Rail
  const view = location.pathname.startsWith('/admin/workspaces') ? 'admin-workspaces'
    : location.pathname.startsWith('/admin/accounts') ? 'admin-accounts'
    : location.pathname.startsWith('/admin') ? 'admin-workspaces'
    : location.pathname.startsWith('/settings') ? 'settings'
    : location.pathname.startsWith('/agents') ? 'agents'
    : location.pathname.startsWith('/schedules') || location.pathname.startsWith('/cron') ? 'schedules'
    : location.pathname.startsWith('/runs') ? 'runs'
    : location.pathname.startsWith('/c/') || location.pathname === '/c' || location.pathname === '/' ? 'chats'
    : '';

  const onNav = (v: string) => {
    if (v === 'chats') navigate('/c');
    else if (v === 'admin-workspaces') navigate('/admin/workspaces');
    else if (v === 'admin-accounts') navigate('/admin/accounts');
    else navigate(`/${v}`);
    setDrawerOpen(false);
  };

  const handleLeaveWorkspace = async () => {
    const user = useAuthStore.getState().user;
    if (!user) {
      useStore.getState().toast('Cannot leave workspace while offline', 'danger');
      return;
    }
    const targetId = tenant.sub || tenant.id;
    try {
      await api.members.remove(targetId, user.id);
      const remaining = useAuthStore
        .getState()
        .memberships.filter((m) => m.workspace_id !== targetId && m.workspace_slug !== targetId);
      useAuthStore.setState({ memberships: remaining });

      if (remaining.length > 0) {
        const next = remaining[0];
        const nextId = next.workspace_slug || next.workspace_id;
        useStore.getState().switchTenant(nextId);
        const nextWs = useStore.getState().db[nextId];
        navigate('/c');
        useStore.getState().toast('Left ' + tenant.name + ' — switched to ' + (next.workspace_name || nextWs?.name || nextId));
      } else {
        navigate('/welcome');
        useStore.getState().toast('Left ' + tenant.name);
      }
    } catch (err: unknown) {
      useStore.getState().toast(formatApiError(err, 'Failed to leave workspace'), 'danger');
    }
  };

  const activeChatId = useStore(s => s.pos.chatId) || '';
  const agentsLoaded = useStore((s: any) => Boolean(s.agentsLoaded?.[s.pos.tenantId]));
  const threadState = useThread(activeChatId);
  const session = threadState.list.find((x: any) => x.id === threadState.active) || null;
  const sessions = threadState.list;
  const { switchSession, newSession, deleteSession } = useStore.getState();
  const railExpanded = useStore((s: any) => Boolean(s.pos.railExpanded));
  const goPos = useStore((s: any) => s.goPos);

  const handleSelectChat = (id: string) => {
    navigate(`/c/${id}`);
    setDrawerOpen(false);
  };

  // A history row must always land in the chat it belongs to — switch the
  // session and make sure the URL points at that chat (e.g. arriving from /c).
  const handleSwitchSession = (sid: string) => {
    switchSession(sid);
    if (activeChatId) navigate(`/c/${activeChatId}`);
  };

  const currentMembership = memberships.find(
    (m) =>
      m.workspace_id === tenant?.id ||
      m.workspace_slug === tenant?.id ||
      m.workspace_slug === tenant?.sub ||
      m.workspace_id === tenant?.sub
  );
  const isSuspended = Boolean(
    currentMembership?.workspace?.disabled_at ||
    (currentMembership as any)?.disabled_at ||
    (tenant as any)?.disabled_at
  );

  const isChatRoute = location.pathname === '/' || location.pathname.startsWith('/c/') || location.pathname === '/c';
  // Only route to onboarding once the workspace's agent list has actually been
  // fetched — an empty list mid-boot means "not loaded yet", not "no agents".
  if (!isSuspended && agentsLoaded && !tenant?.agents?.length && isChatRoute) {
    return <Navigate to="/welcome" replace />;
  }

  const sidebarContent = (
    <Sidebar 
      view={view} 
      tenant={tenant} 
      chatId={activeChatId} 
      onSelect={handleSelectChat}
      activeIsAgent={(tenant?.agents || []).some(a => a.id === activeChatId)}
      session={session}
      sessions={sessions}
      uiRunning={Boolean(ui.running)}
      onSwitchSession={handleSwitchSession}
      onNewSession={newSession} 
      onDeleteSession={deleteSession}
      onDeploy={() => navigate('/agents')}
      onNewSchedule={() => patchUi({ scheduleEdit: 'new' })}
      onEditSchedule={(s: any) => patchUi({ scheduleEdit: s })}
      onOpenSwitcher={() => patchUi({ wsOpen: !ui.wsOpen })}
      search={search} 
      setSearch={setSearch}
    />
  );

  return (
    <div className="flex h-[100dvh] flex-col overflow-hidden font-sans antialiased">
      <ConnectionBanner />
      <div className="flex flex-1 min-h-0 overflow-hidden">
        <Rail
          view={view}
          onNav={onNav}
          tenant={tenant}
          unread={unread}
          onMenuToggle={() => setDrawerOpen(true)}
          onOpenSwitcher={() => patchUi({ wsOpen: !ui.wsOpen })}
          onSettings={() => navigate('/settings')}
          onLogout={handleLogout}
          showAdmin={isAdmin}
          expanded={railExpanded}
          onToggleExpand={() => goPos({ railExpanded: !railExpanded })}
        />
        
        <WorkspaceSwitcher 
          open={ui.wsOpen} 
          memberships={memberships}
          currentId={tenant?.id || tenant?.sub}
          onPick={(id) => {
            useStore.getState().switchTenant(id);
            // switchTenant kicks off loadAgents — land on /c and let the guard
            // decide between chat and onboarding once the fetch resolves.
            navigate('/c');
          }} 
          onClose={() => patchUi({ wsOpen: false })}
          onCreateWorkspace={() => setCreateWsOpen(true)}
        />

        {/* Desktop Sidebar */}
        {!isSuspended && view !== 'agents' && !view.startsWith('admin') && view !== 'settings' && (
          <div className="hidden shrink-0 md:flex">
            {sidebarContent}
          </div>
        )}

        {/* Mobile Drawer */}
        {!isSuspended && (
          <NavDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)}>
            {sidebarContent}
          </NavDrawer>
        )}

        {/* Main Content Area */}
        {isSuspended ? (
          <main className="flex-1 relative flex flex-col items-center justify-center p-6 bg-surface text-center">
            <ErrorState
              variant="full"
              icon="shield"
              title="Workspace suspended"
              description="This workspace has been suspended by an administrator. Please contact your instance administrator or switch to another workspace."
              primaryAction={{
                label: 'Switch workspace',
                onClick: () => patchUi({ wsOpen: true }),
              }}
            />
          </main>
        ) : (
          <main className="flex-1 relative flex overflow-hidden bg-surface">
            <ErrorBoundary mode="shell">
              <Routes>
                <Route path="/admin/workspaces" element={<AdminView screen="workspaces" tenant={tenant} />} />
                <Route path="/admin/accounts" element={<AdminView screen="accounts" tenant={tenant} />} />
                <Route path="/admin/tenants" element={<Navigate to="/admin/workspaces" replace />} />
                <Route path="/admin/users" element={<Navigate to="/admin/accounts" replace />} />
                <Route path="/admin/superadmins" element={<Navigate to="/admin/accounts" replace />} />
                <Route path="/admin" element={<Navigate to="/admin/workspaces" replace />} />
                <Route path="/admin/:tab" element={<Navigate to="/admin/workspaces" replace />} />
                <Route path="/settings" element={<Navigate to="/settings/workspace" replace />} />
                <Route
                  path="/settings/:section"
                  element={
                    <SettingsPage
                      tenant={tenant}
                      onToast={useStore.getState().toast}
                      onUpdate={(fn: any) => useStore.getState().updateTenant(tenant.id, fn)}
                      onLeaveWorkspace={handleLeaveWorkspace}
                    />
                  }
                />
                <Route path="/c" element={<ChatRoute />} />
                <Route path="/c/:chatId" element={<ChatRoute />} />
                <Route path="/agents" element={<AgentsView tenant={tenant} onChat={handleSelectChat} onConfigure={(id: string) => patchUi({ configAgent: id })} onDeploy={() => patchUi({ configAgent: 'new' })} />} />
                <Route path="/schedules" element={<SchedulesView tenant={tenant} onEdit={(s: any) => patchUi({ scheduleEdit: s })} onNew={() => patchUi({ scheduleEdit: 'new' })} onToast={useStore.getState().toast} />} />
                {/* Pre-rename deep links land on the schedules screen. */}
                <Route path="/cron" element={<Navigate to="/schedules" replace />} />
                <Route path="/runs" element={<RunsView tenant={tenant} onToast={useStore.getState().toast} />} />
                <Route path="/welcome" element={<OnboardingPane tenant={tenant} onDeploy={() => patchUi({ configAgent: 'new' })} onSettings={() => navigate('/settings')} />} />
                {/* Home is the chat page with nothing pre-opened; pick a conversation from the sidebar. */}
                <Route path="/" element={<Navigate to="/c" replace />} />
                <Route path="*" element={<NotFoundRoute />} />
              </Routes>
            </ErrorBoundary>
          </main>
        )}
      </div>

      {/* Modals */}
      
      {ui.configAgent && (
        <AgentConfigModal 
          key={ui.configAgent}
          draft={ui.configAgent === 'new' ? null : (tenant.agents.find((a: any) => a.id === ui.configAgent) || null)}
          tenant={tenant}
          onClose={() => patchUi({ configAgent: null })} 
          onSave={(values: any) => {
            const isNew = ui.configAgent === 'new';
            const agentId = useStore.getState().upsertAgent(values);
            if (isNew && agentId) {
              navigate(`/c/${agentId}`);
            }
          }}
        />
      )}

      
      {ui.scheduleEdit && (
        <ScheduleEditorModal
          draft={ui.scheduleEdit === 'new' ? null : ui.scheduleEdit}
          tenant={tenant}
          onClose={() => patchUi({ scheduleEdit: null })}
          onChanged={() => useStore.getState().loadSchedules(tenant.id || tenant.sub)}
        />
      )}

      {createWsOpen && (
        <CreateWorkspaceModal
          onClose={() => setCreateWsOpen(false)}
          onCreateSuccess={(res) => {
            const nextId = res.workspace.slug || res.workspace.id;
            useStore.getState().switchTenant(nextId);
            if (res.starter_agent?.id) {
              navigate(`/c/${res.starter_agent.id}`);
            } else {
              navigate('/welcome');
            }
          }}
        />
      )}

      <Toasts toasts={ui.toasts} />
    </div>
  );
}

export default function App() {
  return (
    <ErrorBoundary mode="full">
      <BrowserRouter>
        <BootGate>
          <Routes>
            <Route path="/login" element={<LoginView />} />
            <Route
              path="/*"
              element={
                <RequireAuth>
                  <Layout />
                </RequireAuth>
              }
            />
          </Routes>
        </BootGate>
      </BrowserRouter>
    </ErrorBoundary>
  );
}


