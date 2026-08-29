import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useNavigate, useLocation, useParams } from 'react-router-dom';
import { useStore, useWorkspace, useSearchShortcut, useThread } from './store';
import { useAuthStore, useAuth, useIsAdmin } from './store/auth';
import { AssistantRuntimeProvider } from '@assistant-ui/react';
import { useChatRuntime } from './chat/runtime';
import { Rail } from './components/nav/Rail';
import { Sidebar } from './components/nav/Sidebar';
import { WorkspaceSwitcher } from './components/nav/WorkspaceSwitcher';
import { NavDrawer } from './components/nav/NavDrawer';
import { Toasts } from './components/ui/Toasts';
import { Icon } from './components/ui/Icon';
import { ChatView } from './components/chat/ChatView';
import { ContextPanel } from './components/chat/ContextPanel';
import { AgentsView } from './screens/AgentsView';
import { CronView } from './screens/CronView';
import { RunsView } from './screens/RunsView';
import { OnboardingPane } from './screens/OnboardingPane';
import { LoginView } from './screens/LoginView';
import { AdminView } from './screens/admin/AdminView';
import { SettingsModal } from './modals/SettingsModal';
import { AgentConfigModal } from './modals/AgentConfigModal';
import { CronEditorModal } from './modals/CronEditorModal';
import { SKILLS } from "./lib/constants";
import { blankTenant } from "./data/seed";
import { api, formatApiError, type ApiMemberView } from "./lib/api";

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

  const handleLogout = async () => {
    await logout();
    navigate('/login', { replace: true });
  };

  // Map route to view string for Rail
  const view = location.pathname.startsWith('/admin/workspaces') ? 'admin-workspaces'
    : location.pathname.startsWith('/admin/accounts') ? 'admin-accounts'
    : location.pathname.startsWith('/admin') ? 'admin-workspaces'
    : location.pathname.startsWith('/agents') ? 'agents' 
    : location.pathname.startsWith('/cron') ? 'cron' 
    : location.pathname.startsWith('/runs') ? 'runs' 
    : location.pathname.startsWith('/c/') || location.pathname === '/' ? 'chats'
    : '';

  const onNav = (v: string) => {
    if (v === 'chats') navigate(`/c/${tenant?.agents?.[0]?.id || ''}`);
    else if (v === 'admin-workspaces') navigate('/admin/workspaces');
    else if (v === 'admin-accounts') navigate('/admin/accounts');
    else navigate(`/${v}`);
    setDrawerOpen(false);
  };

  const activeChatId = useStore(s => s.pos.chatId) || tenant?.agents?.[0]?.id;
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
  if (!isSuspended && !tenant?.agents?.length && isChatRoute) {
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
      onSwitchSession={switchSession} 
      onNewSession={newSession} 
      onDeleteSession={deleteSession}
      onDeploy={() => navigate('/agents')} 
      onNewSchedule={() => {}}
      onEditCron={() => {}}
      onOpenSwitcher={() => patchUi({ wsOpen: !ui.wsOpen })}
      search={search} 
      setSearch={setSearch}
    />
  );

  return (
    <div className="flex h-[100dvh] overflow-hidden font-sans antialiased">
      <Rail
        view={view}
        onNav={onNav}
        tenant={tenant}
        unread={unread}
        onMenuToggle={() => setDrawerOpen(true)}
        onOpenSwitcher={() => patchUi({ wsOpen: !ui.wsOpen })}
        onSettings={() => patchUi({ settingsOpen: true, settingsTab: 'workspace' })}
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
          const next = useStore.getState().db[id];
          navigate(next?.agents?.length ? `/c/${next.agents[0].id}` : '/welcome');
        }} 
        onClose={() => patchUi({ wsOpen: false })}
      />

      {/* Desktop Sidebar */}
      {!isSuspended && view !== 'agents' && !view.startsWith('admin') && (
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
          <div className="max-w-md space-y-3">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--danger)_15%,transparent)] text-danger">
              <Icon name="shield" size={24} />
            </div>
            <h2 className="text-[18px] font-semibold text-fg">Workspace suspended</h2>
            <p className="text-[13px] leading-5 text-muted">
              This workspace has been suspended by an administrator. Please contact your instance administrator or switch to another workspace.
            </p>
            <div className="pt-2">
              <button
                type="button"
                onClick={() => patchUi({ wsOpen: true })}
                className="inline-flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)]"
              >
                Switch workspace
              </button>
            </div>
          </div>
        </main>
      ) : (
        <main className="flex-1 relative flex overflow-hidden bg-surface">
          <Routes>
            <Route path="/admin/workspaces" element={<AdminView screen="workspaces" tenant={tenant} />} />
            <Route path="/admin/accounts" element={<AdminView screen="accounts" tenant={tenant} />} />
            <Route path="/admin/tenants" element={<Navigate to="/admin/workspaces" replace />} />
            <Route path="/admin/users" element={<Navigate to="/admin/accounts" replace />} />
            <Route path="/admin/superadmins" element={<Navigate to="/admin/accounts" replace />} />
            <Route path="/admin" element={<Navigate to="/admin/workspaces" replace />} />
            <Route path="/admin/:tab" element={<Navigate to="/admin/workspaces" replace />} />
            <Route path="/c/:chatId" element={<ChatRoute />} />
            <Route path="/agents" element={<AgentsView tenant={tenant} onChat={handleSelectChat} onConfigure={(id: string) => patchUi({ configAgent: id })} onDeploy={() => patchUi({ configAgent: 'new' })} />} />
            <Route path="/cron" element={<CronView tenant={tenant} onEdit={(j: any) => patchUi({ cronEdit: j })} onToggle={(j: any) => useStore.getState().toggleCron(j)} onRunNow={(j: any) => useStore.getState().runNow(j)} onNew={() => patchUi({ cronEdit: { id: null, name: '', agentId: tenant?.agents?.[0]?.id, expr: '0 9 * * 1-5', human: '', enabled: true } })} />} />
            <Route path="/runs" element={<RunsView tenant={tenant} />} />
            <Route path="/welcome" element={<OnboardingPane tenant={tenant} onDeploy={() => patchUi({ configAgent: 'new' })} onSettings={() => patchUi({ settingsOpen: true, settingsTab: 'workspace' })} />} />
            <Route path="/" element={<Navigate to={useStore.getState().pos.view === 'chats' && useStore.getState().pos.chatId ? `/c/${useStore.getState().pos.chatId}` : `/${useStore.getState().pos.view || 'agents'}`} replace />} />
            <Route path="*" element={<Navigate to={`/c/${tenant?.agents?.[0]?.id || ''}`} replace />} />
          </Routes>
        </main>
      )}

      {/* Modals */}
      {ui.settingsOpen && (
        <SettingsModal 
          tenant={tenant} 
          tab={ui.settingsTab || 'workspace'} 
          onTab={(t: string) => patchUi({ settingsTab: t })}
          onClose={() => patchUi({ settingsOpen: false })}
          onUpdate={(fn: any) => useStore.getState().updateTenant(tenant.id, fn)} 
          onToast={useStore.getState().toast} 
          onLeaveWorkspace={async () => {
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
              patchUi({ settingsOpen: false });

              if (remaining.length > 0) {
                const next = remaining[0];
                const nextId = next.workspace_slug || next.workspace_id;
                useStore.getState().switchTenant(nextId);
                const nextWs = useStore.getState().db[nextId];
                navigate(nextWs?.agents?.length ? `/c/${nextWs.agents[0].id}` : '/welcome');
                useStore.getState().toast('Left ' + tenant.name + ' — switched to ' + (next.workspace_name || nextWs?.name || nextId));
              } else {
                navigate('/welcome');
                useStore.getState().toast('Left ' + tenant.name);
              }
            } catch (err: unknown) {
              useStore.getState().toast(formatApiError(err, 'Failed to leave workspace'), 'danger');
            }
          }}
        />
      )}
      
      {ui.configAgent && (
        <AgentConfigModal 
          key={ui.configAgent}
          draft={ui.configAgent === 'new' ? null : (tenant.agents.find((a: any) => a.id === ui.configAgent) || null)}
          skillOptions={SKILLS.concat((tenant.skillLib || []).filter((s: any) => !SKILLS.some((r: any) => r.id === s.id)).map((s: any) => ({ id: s.id, label: s.name })))}
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
      
      {ui.cronEdit && (
        <CronEditorModal 
          job={ui.cronEdit} 
          tenant={tenant}
          onClose={() => patchUi({ cronEdit: null })} 
          onSave={(draft: any) => useStore.getState().saveCron(draft)} 
          onDelete={(job: any) => useStore.getState().deleteCron(job)}
        />
      )}

      <Toasts toasts={ui.toasts} />
    </div>
  );
}

function ChatRoute() {
  const { chatId } = useParams();
  const navigate = useNavigate();
  const tenant = useWorkspace();
  const pos = useStore((s: any) => s.pos);
  const goPos = useStore((s: any) => s.goPos);
  const ui = useStore((s: any) => s.ui);

  const toast = useStore((s: any) => s.toast);
  const chatRuntime = useChatRuntime(chatId!);

  useEffect(() => {
    if (chatId && chatId !== pos.chatId) {
      useStore.getState().selectChat(chatId);
    }
  }, [chatId, pos.chatId]);

  // Actions
  const addChannelMember = (id: string) => useStore.getState().addChannelMember(chatId!, id);
  const removeChannelMember = (id: string) => useStore.getState().removeChannelMember(chatId!, id);
  const openMember = (id: string) => navigate(`/c/${id}`);

  const agent = (tenant?.agents || []).find((a: any) => a.id === chatId) || null;
  const channel = (tenant?.channels || []).find((c) => c.id === chatId) || null;
  const person = (tenant?.people || []).find((p) => p.id === chatId) || null;
  const chatAgent = agent || (channel ? (tenant?.agents || []).find((a: any) => a.id === channel.agentId) || null : null);
  
  const threadState = useThread(chatId!);
  
  const valid = !!(agent || channel || person);
  if (!valid && (tenant?.agents || []).length > 0) {
    return <Navigate to={`/c/${tenant.agents[0].id}`} replace />;
  }
  const target = agent ? { kind: 'agent', obj: agent } : channel ? { kind: 'channel', obj: channel } : { kind: 'person', obj: person };

  const session = threadState.list.find((x: any) => x.id === threadState.active) || null;
  const thread = session ? session.messages : [];

  const channelMembers = channel ? (() => {
    const ids = channel.members && channel.members.length ? channel.members : (channel.agentId ? [channel.agentId] : []);
    const out: any[] = [];
    ids.forEach((id: string) => {
      if (out.some((m: any) => m.id === id)) return;
      const a = (tenant?.agents || []).find((x: any) => x.id === id);
      if (a) { out.push({ id, kind: 'agent', name: a.name, agent: a }); return; }
      const p = (tenant?.people || []).find((x: any) => x.id === id);
      if (p) out.push({ id, kind: 'person', name: p.name, presence: p.presence });
    });
    return out;
  })() : null;

  const memberCandidates = channel ? (() => {
    const inCh = new Set([...(channel.members || []), channel.agentId].filter(Boolean));
    const out: any[] = [];
    (tenant?.agents || []).forEach((a: any) => { if (!inCh.has(a.id)) out.push({ id: a.id, kind: 'agent', name: a.name, agent: a }); });
    (tenant?.people || []).forEach((p) => { if (!inCh.has(p.id)) out.push({ id: p.id, kind: 'person', name: p.name, presence: p.presence }); });
    return out;
  })() : [];

  const copyText = (text: string) => {
    if (navigator.clipboard) navigator.clipboard.writeText(text).then(() => toast('Copied to clipboard'), () => toast('Clipboard blocked by the browser', 'danger'));
    else toast('Clipboard unavailable in this frame', 'danger');
  };

  return (
    <>
      <AssistantRuntimeProvider runtime={chatRuntime.runtime}>
        <ChatView 
          tenant={tenant} 
          target={target} 
          agent={chatAgent} 
          thread={thread} 
          session={session} 
          channelMembers={channelMembers} 
          onToggleMembers={() => goPos({ showContext: !pos.showContext })}
          typing={ui.running} 
          busy={ui.running}
          onSend={(text: string) => chatRuntime.onNew({ role: 'user', content: [{ type: 'text', text }] } as unknown as import('@assistant-ui/react').AppendMessage)} 
          onCancel={chatRuntime.onCancel}
          onConfigure={() => useStore.getState().patchUi({ configAgent: chatAgent?.id })}
          onCopy={copyText} 
          onRefresh={chatRuntime.onReload} 
          onBranch={chatRuntime.branchNav} 
          onEditSubmit={(mid: string, text: string) => chatRuntime.onEdit({ sourceId: mid, role: 'user', content: [{ type: 'text', text }] } as unknown as import('@assistant-ui/react').AppendMessage)}
          onAttach={() => useStore.getState().toast('Attachments arrive with the storage integration — connect it in Settings → Integrations')}
        />
      </AssistantRuntimeProvider>
      {pos.showContext && target.kind === 'channel' && (
        <>
          {/* Static column >= 1280px (xl in Tailwind v3, but we might just use xl:flex) */}
          <div className="hidden xl:flex">
            <ContextPanel 
              channelMembers={channelMembers} 
              candidates={memberCandidates} 
              primaryAgentId={channel?.agentId}
              onAddMember={addChannelMember} 
              onRemoveMember={removeChannelMember}
              onClose={() => goPos({ showContext: false })} 
              onOpenMember={openMember}
            />
          </div>
          {/* Slide-over sheet < 1280px */}
          <div className="fixed inset-0 z-40 flex xl:hidden" aria-modal="true" role="dialog">
            <div className="od-fade absolute inset-0 bg-[color-mix(in_oklab,var(--fg)_32%,transparent)]" onClick={() => goPos({ showContext: false })} />
            <div className="od-pop relative flex w-[300px] max-w-[85vw] ml-auto h-full flex-col bg-bg shadow-[var(--elev-raised)]">
              <ContextPanel 
                channelMembers={channelMembers} 
                candidates={memberCandidates} 
                primaryAgentId={channel?.agentId}
                onAddMember={addChannelMember} 
                onRemoveMember={removeChannelMember}
                onClose={() => goPos({ showContext: false })} 
                onOpenMember={openMember}
              />
            </div>
          </div>
        </>
      )}
    </>
  );
}

export default function App() {
  return (
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
  );
}

