import { useEffect, useState } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useNavigate, useLocation, useParams } from 'react-router-dom';
import { useStore, useWorkspace, useSearchShortcut } from './store';
import { Rail } from './components/nav/Rail';
import { Sidebar } from './components/nav/Sidebar';
import { WorkspaceSwitcher } from './components/nav/WorkspaceSwitcher';
import { NavDrawer } from './components/nav/NavDrawer';
import { Toasts } from './components/ui/Toasts';
import { ChatView } from './components/chat/ChatView';
import { ContextPanel } from './components/chat/ContextPanel';
import { AgentsView } from './screens/AgentsView';
import { CronView } from './screens/CronView';
import { RunsView } from './screens/RunsView';
import { OnboardingPane } from './screens/OnboardingPane';
import { SettingsModal } from './modals/SettingsModal';
import { AgentConfigModal } from './modals/AgentConfigModal';
import { CronEditorModal } from './modals/CronEditorModal';
import { CreateWorkspaceModal } from './modals/CreateWorkspaceModal';
import { SKILLS } from "./lib/constants";

function Layout() {
  useSearchShortcut();
  const navigate = useNavigate();
  const location = useLocation();
  const tenant = useWorkspace();
  
  const ui = useStore((s: any) => s.ui);
  const patchUi = useStore((s: any) => s.patchUi);
  const search = useStore((s: any) => s.search);
  const setSearch = useStore((s: any) => s.setSearch);
  const unread = tenant.channels.reduce((n, c) => n + (c.unread || 0), 0);

  const [drawerOpen, setDrawerOpen] = useState(false);

  // Map route to view string for Rail
  const view = location.pathname.startsWith('/agents') ? 'agents' 
    : location.pathname.startsWith('/cron') ? 'cron' 
    : location.pathname.startsWith('/runs') ? 'runs' 
    : 'chats';

  const onNav = (v: string) => {
    if (v === 'chats') navigate(`/c/${tenant.agents[0]?.id || ''}`);
    else navigate(`/${v}`);
    setDrawerOpen(false);
  };

  const activeChatId = useStore(s => s.pos.chatId) || tenant.agents[0]?.id;
  const EMPTY_THREAD_STATE = { active: null, list: [] };
  const rawTh = useStore(s => s.db[s.pos.tenantId]?.threads[activeChatId as string]);
  const threadState = Array.isArray(rawTh) ? (rawTh.length ? { active: 's0', list: [{ id: 's0', title: 'Chat', updated: '', messages: rawTh }] } : EMPTY_THREAD_STATE) : (rawTh || EMPTY_THREAD_STATE);
  const session = threadState.list.find((x: any) => x.id === threadState.active) || null;
  const sessions = threadState.list;
  const { switchSession, newSession, deleteSession } = useStore.getState();

  const handleSelectChat = (id: string) => {
    navigate(`/c/${id}`);
    setDrawerOpen(false);
  };

  if (!tenant.agents.length && location.pathname !== '/welcome') {
    return <Navigate to="/welcome" replace />;
  }

  const sidebarContent = (
    <Sidebar 
      view={view} 
      tenant={tenant} 
      chatId={activeChatId} 
      onSelect={handleSelectChat}
      activeIsAgent={tenant.agents.some(a => a.id === activeChatId)}
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

  // Modal state bindings
  
  return (
    <div className="flex h-[100dvh] overflow-hidden font-sans antialiased">
      <Rail 
        view={view} 
        onNav={onNav} 
        tenant={tenant} 
        unread={unread}
        onOpenSwitcher={() => patchUi({ wsOpen: !ui.wsOpen })}
        onSettings={() => patchUi({ settingsOpen: true, settingsTab: 'workspace' })}
      />
      
      <WorkspaceSwitcher 
        open={ui.wsOpen} 
        db={useStore.getState().db} 
        currentId={tenant.id}
        onPick={(id) => {
          useStore.getState().switchTenant(id);
          const next = useStore.getState().db[id];
          navigate(next.agents.length ? `/c/${next.agents[0].id}` : '/welcome');
        }} 
        onClose={() => patchUi({ wsOpen: false })}
        onCreate={() => patchUi({ wsOpen: false, createWsOpen: true })}
      />

      {/* Desktop Sidebar */}
      {view !== 'agents' && (
        <div className="hidden shrink-0 md:flex">
          {sidebarContent}
        </div>
      )}

      {/* Mobile Drawer */}
      <NavDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)}>
        {sidebarContent}
      </NavDrawer>

      {/* Main Content Area */}
      <main className="flex-1 relative flex overflow-hidden bg-surface">
        

        <Routes>
          <Route path="/c/:chatId" element={<ChatRoute />} />
          <Route path="/agents" element={<AgentsView tenant={tenant} onChat={handleSelectChat} onConfigure={(id: string) => patchUi({ configAgent: id })} onDeploy={() => patchUi({ configAgent: 'new' })} />} />
          <Route path="/cron" element={<CronView tenant={tenant} onEdit={(j: any) => patchUi({ cronEdit: j })} onToggle={(j: any) => useStore.getState().toggleCron(j)} onRunNow={(j: any) => useStore.getState().runNow(j)} onNew={() => patchUi({ cronEdit: { id: null, name: '', agentId: tenant.agents[0]?.id, expr: '0 9 * * 1-5', human: '', enabled: true } })} />} />
          <Route path="/runs" element={<RunsView tenant={tenant} />} />
          <Route path="/welcome" element={<OnboardingPane tenant={tenant} onDeploy={() => patchUi({ configAgent: 'new' })} onSettings={() => patchUi({ settingsOpen: true, settingsTab: 'workspace' })} />} />
          <Route path="/" element={<Navigate to={useStore.getState().pos.view === 'chats' && useStore.getState().pos.chatId ? `/c/${useStore.getState().pos.chatId}` : `/${useStore.getState().pos.view || 'agents'}`} replace />} />
          <Route path="*" element={<Navigate to={`/c/${tenant.agents[0]?.id || ''}`} replace />} />
        </Routes>
      </main>

      {/* Modals */}
      {ui.settingsOpen && (
        <SettingsModal 
          tenant={tenant} 
          tab={ui.settingsTab || 'workspace'} 
          onTab={(t: string) => patchUi({ settingsTab: t })}
          onClose={() => patchUi({ settingsOpen: false })}
          onUpdate={(fn: any) => useStore.getState().updateTenant(tenant.id, fn)} 
          onToast={useStore.getState().toast} 
          onDeleteWorkspace={() => {
            useStore.getState().deleteWorkspace(tenant.id);
            const next = useStore.getState().db[Object.keys(useStore.getState().db)[0]];
            if (next) navigate(next.agents.length ? `/c/${next.agents[0].id}` : '/welcome');
          }}
        />
      )}
      
      {ui.configAgent && (
        <AgentConfigModal 
          key={ui.configAgent}
          draft={ui.configAgent === 'new' ? null : (tenant.agents.find((a: any) => a.id === ui.configAgent) || null)}
          skillOptions={SKILLS.concat((tenant.skillLib || []).filter((s: any) => !SKILLS.some((r: any) => r.id === s.id)).map((s: any) => ({ id: s.id, label: s.name })))}
          onClose={() => patchUi({ configAgent: null })} 
          onSave={(values: any) => useStore.getState().upsertAgent(values)}
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
      
      {ui.createWsOpen && (
        <CreateWorkspaceModal 
          onClose={() => patchUi({ createWsOpen: false })} 
          onCreate={(ws: any) => {
            useStore.getState().createWorkspace(ws);
            navigate(ws.agents.length ? `/c/${ws.agents[0].id}` : '/welcome');
          }}
          existingSubs={Object.values(useStore.getState().db).map((t: any) => t.sub)}
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
  const patchUi = useStore((s: any) => s.patchUi);
  const toast = useStore((s: any) => s.toast);

  useEffect(() => {
    if (chatId && chatId !== pos.chatId) {
      useStore.getState().selectChat(chatId);
    }
  }, [chatId, pos.chatId]);

  // Actions
  const send = useStore((s: any) => s.send);
  const refreshMessage = useStore((s: any) => s.refreshMessage);
  const branchNav = useStore((s: any) => s.branchNav);
  const editSubmit = useStore((s: any) => s.editSubmit);
  const addChannelMember = (id: string) => useStore.getState().addChannelMember(chatId!, id);
  const removeChannelMember = (id: string) => useStore.getState().removeChannelMember(chatId!, id);
  const openMember = (id: string) => navigate(`/c/${id}`);

  const agent = tenant.agents.find((a: any) => a.id === chatId) || null;
  const channel = tenant.channels.find((c) => c.id === chatId) || null;
  const person = tenant.people.find((p) => p.id === chatId) || null;
  const chatAgent = agent || (channel ? tenant.agents.find((a: any) => a.id === channel.agentId) || null : null);
  
  const valid = !!(agent || channel || person);
  if (!valid && tenant.agents.length > 0) {
    return <Navigate to={`/c/${tenant.agents[0].id}`} replace />;
  }
  const target = agent ? { kind: 'agent', obj: agent } : channel ? { kind: 'channel', obj: channel } : { kind: 'person', obj: person };

  const rawThChat = useStore((s: any) => s.db[s.pos.tenantId]?.threads[chatId as string]);
  const threadState = Array.isArray(rawThChat) ? (rawThChat.length ? { active: 's0', list: [{ id: 's0', title: 'Chat', updated: '', messages: rawThChat }] } : { active: null, list: [] }) : (rawThChat || { active: null, list: [] });
  const session = threadState.list.find((x: any) => x.id === threadState.active) || null;
  const thread = session ? session.messages : [];

  const channelMembers = channel ? (() => {
    const ids = channel.members && channel.members.length ? channel.members : (channel.agentId ? [channel.agentId] : []);
    const out: any[] = [];
    ids.forEach((id: string) => {
      if (out.some((m: any) => m.id === id)) return;
      const a = tenant.agents.find((x: any) => x.id === id);
      if (a) { out.push({ id, kind: 'agent', name: a.name, agent: a }); return; }
      const p = tenant.people.find((x: any) => x.id === id);
      if (p) out.push({ id, kind: 'person', name: p.name, presence: p.presence });
    });
    return out;
  })() : null;

  const memberCandidates = channel ? (() => {
    const inCh = new Set([...(channel.members || []), channel.agentId].filter(Boolean));
    const out: any[] = [];
    tenant.agents.forEach((a: any) => { if (!inCh.has(a.id)) out.push({ id: a.id, kind: 'agent', name: a.name, agent: a }); });
    tenant.people.forEach((p) => { if (!inCh.has(p.id)) out.push({ id: p.id, kind: 'person', name: p.name, presence: p.presence }); });
    return out;
  })() : [];

  const copyText = (text: string) => {
    if (navigator.clipboard) navigator.clipboard.writeText(text).then(() => toast('Copied to clipboard'), () => toast('Clipboard blocked by the browser', 'danger'));
    else toast('Clipboard unavailable in this frame', 'danger');
  };

  return (
    <>
      <ChatView 
        tenant={tenant} 
        target={target} 
        agent={chatAgent} 
        thread={thread}
        session={session} 
        channelMembers={channelMembers} 
        onToggleMembers={() => goPos({ showContext: !pos.showContext })}
        typing={ui.typing} 
        streamingId={ui.streamId} 
        busy={ui.typing || !!ui.streamId}
        onSend={send} 
        onCancel={() => useStore.getState().cancelReply()}
        onDoneStream={() => patchUi({ streamId: null })}
        onConfigure={() => patchUi({ configAgent: chatAgent?.id })}
        onCopy={copyText} 
        onRefresh={refreshMessage} 
        onBranch={branchNav} 
        onEditSubmit={editSubmit}
        onAttach={() => toast('Attachments arrive with the storage integration — connect it in Settings → Integrations')}
      />
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
      <Layout />
    </BrowserRouter>
  );
}
