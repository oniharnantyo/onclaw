import { useState, useEffect } from 'react';
import {
  Terminal,
  ChatCircle,
  ClockCounterClockwise,
  Key,
  Robot,
  SignOut,
  WarningCircle,
  CheckCircle,
  Code,
  Lightning,
  Plug,
  Wrench,
} from '@phosphor-icons/react';

import Login from './components/Login';
import Providers from './components/Providers';
import type { Provider } from './components/Providers';
import Agents from './components/Agents';
import type { Agent } from './components/Agents';
import Conversations from './components/Conversations';
import type { Conversation } from './components/Conversations';
import Chat from './components/Chat';
import type { Message } from './components/MessageBubble';
import Skills from './components/Skills';
import type { Skill } from './components/Skills';
import Hooks from './components/Hooks';
import MCP from './components/MCP';
import Tools from './components/Tools';

type Tab = 'chat' | 'conversations' | 'providers' | 'agents' | 'skills' | 'hooks' | 'mcp' | 'tools';


interface NavItem {
  id: Tab;
  label: string;
  icon: React.ReactNode;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

interface Toast {
  id: number;
  message: string;
  type: 'success' | 'error';
}

export default function App() {
  const [isLoggedIn, setIsLoggedIn] = useState<boolean | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>('chat');
  const [toasts, setToasts] = useState<Toast[]>([]);

  // Data States
  const [providers, setProviders] = useState<Provider[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [activeConvID, setActiveConvID] = useState<number | null>(null);
  const [chatAgent, setChatAgent] = useState('');

  // Toast Helper
  const showToast = (message: string, type: 'success' | 'error' = 'success') => {
    const id = Date.now();
    setToasts((prev) => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 4000);
  };

  // Auth check on mount
  useEffect(() => {
    fetch('/api/health')
      .then((res) => {
        if (res.status === 401) {
          setIsLoggedIn(false);
        } else {
          return fetch('/api/providers').then((provRes) => {
            if (provRes.status === 401) {
              setIsLoggedIn(false);
            } else {
              setIsLoggedIn(true);
            }
          });
        }
      })
      .catch(() => setIsLoggedIn(false));
  }, []);

  // Fetch initial data when logged in
  useEffect(() => {
    if (isLoggedIn === true) {
      loadProviders();
      loadAgents();
      loadConversations();
      loadSkills();
    }
  }, [isLoggedIn]);

  const handleLogout = async () => {
    try {
      await fetch('/api/logout', { method: 'POST' });
      setIsLoggedIn(false);
      showToast('Logged out successfully');
    } catch {
      setIsLoggedIn(false);
    }
  };

  const loadProviders = async () => {
    try {
      const res = await fetch('/api/providers');
      if (res.ok) {
        setProviders(await res.json());
      }
    } catch {
      showToast('Failed to load providers', 'error');
    }
  };

  const loadAgents = async () => {
    try {
      const res = await fetch('/api/agents');
      if (res.ok) {
        const data = await res.json();
        setAgents(data);
        if (data.length > 0) {
          const defAgent = data.find((a: Agent) => a.is_default) || data[0];
          setChatAgent(defAgent.name);
        }
      }
    } catch {
      showToast('Failed to load agents', 'error');
    }
  };

  const loadConversations = async () => {
    try {
      const res = await fetch('/api/conversations');
      if (res.ok) {
        setConversations(await res.json());
      }
    } catch {
      showToast('Failed to load conversations', 'error');
    }
  };

  const loadSkills = async () => {
    try {
      const res = await fetch('/api/skills');
      if (res.ok) {
        const data = await res.json();
        setSkills(data || []);
      }
    } catch {
      showToast('Failed to load skills', 'error');
    }
  };

  const loadMessages = async (convId: number) => {
    try {
      const res = await fetch(`/api/conversations/${convId}/messages`);
      if (res.ok) {
        const rawMsgs = await res.json();
        const parsedMsgs: Message[] = rawMsgs.map((m: any) => {
          try {
            const parsed = JSON.parse(m.Message);
            return {
              id: m.ID,
              seq: m.Seq,
              role: m.Role,
              content_blocks: parsed.content_blocks || [{ type: 'text', assistant_gen_text: { text: parsed.content || '' } }],
              created_at: m.CreatedAt
            };
          } catch {
            return {
              id: m.ID,
              seq: m.Seq,
              role: m.Role,
              content_blocks: [{ type: 'text', assistant_gen_text: { text: m.Message } }],
              created_at: m.CreatedAt
            };
          }
        });
        setMessages(parsedMsgs);
      }
    } catch {
      showToast('Failed to load conversation history', 'error');
    }
  };

  const selectConversation = (id: number) => {
    setActiveConvID(id);
    loadMessages(id);
    setActiveTab('chat');
  };

  // Loading state
  if (isLoggedIn === null) {
    return (
      <div className="loading-screen">
        <div className="loading-spinner">
          <div className="spinner" role="status" aria-label="Loading" />
          <span style={{ color: 'var(--text-muted)', fontSize: '13px' }}>Connecting…</span>
        </div>
      </div>
    );
  }

  if (isLoggedIn === false) {
    return <Login onLoginSuccess={() => setIsLoggedIn(true)} showToast={showToast} />;
  }

  const NAV_GROUPS: NavGroup[] = [
    {
      label: 'Chat',
      items: [
        { id: 'chat',          label: 'Live Chat',  icon: <ChatCircle weight="duotone" size={18} /> },
        { id: 'conversations', label: 'History',    icon: <ClockCounterClockwise weight="duotone" size={18} /> },
      ],
    },
    {
      label: 'Agent',
      items: [
        { id: 'agents',        label: 'Agents',     icon: <Robot weight="duotone" size={18} /> },
        { id: 'skills',        label: 'Skills',     icon: <Code weight="duotone" size={18} /> },
        { id: 'hooks',         label: 'Hooks',      icon: <Lightning weight="duotone" size={18} /> },
        { id: 'mcp',           label: 'MCP Servers',icon: <Plug weight="duotone" size={18} /> },
        { id: 'tools',         label: 'Tools',      icon: <Wrench weight="duotone" size={18} /> },
      ],
    },
    {
      label: 'Configuration',
      items: [
        { id: 'providers',     label: 'Providers',  icon: <Key weight="duotone" size={18} /> },
      ],
    },
  ];

  const HEADER_TITLES: Record<Tab, string> = {
    chat:          'Live Agent Session',
    conversations: 'Conversation History',
    providers:     'LLM Providers',
    agents:        'AI Agents',
    skills:        'Agent Skills',
    hooks:         'Lifecycle Hooks',
    mcp:           'MCP Servers',
    tools:         'Builtin Tools',
  };


  return (
    <div className="app-container">
      {/* Toast notifications */}
      <div className="toast-container" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast-${t.type}`} role="alert">
            {t.type === 'error'
              ? <WarningCircle size={17} weight="fill" aria-hidden />
              : <CheckCircle size={17} weight="fill" aria-hidden />
            }
            <span>{t.message}</span>
          </div>
        ))}
      </div>

      {/* Sidebar */}
      <aside className="sidebar" role="navigation" aria-label="Main navigation">
        <div className="sidebar-header">
          <div className="logo-icon" aria-hidden="true">
            <Terminal size={18} weight="bold" />
          </div>
          <div>
            <div className="logo-text">ONCLAW</div>
            <div className="logo-tagline">AI Console</div>
          </div>
        </div>

        <nav className="sidebar-nav">
          {NAV_GROUPS.map((group) => (
            <div key={group.label} className="nav-group">
              <div className="nav-section-label">{group.label}</div>
              {group.items.map((item) => (
                <button
                  key={item.id}
                  id={`nav-${item.id}`}
                  className={`nav-item ${activeTab === item.id ? 'active' : ''}`}
                  onClick={() => setActiveTab(item.id)}
                  aria-current={activeTab === item.id ? 'page' : undefined}
                >
                  <span className="nav-item-icon" aria-hidden="true">{item.icon}</span>
                  {item.label}
                </button>
              ))}
            </div>
          ))}
        </nav>

        <div className="sidebar-footer">
          <button
            id="nav-signout"
            className="nav-item"
            onClick={handleLogout}
            style={{ width: '100%' }}
            aria-label="Sign out"
          >
            <span className="nav-item-icon" aria-hidden="true">
              <SignOut size={18} weight="duotone" />
            </span>
            Sign Out
          </button>
        </div>
      </aside>

      {/* Main Content */}
      <main className="main-content" role="main">
        <header className="main-header">
          <div className="header-title">{HEADER_TITLES[activeTab]}</div>
          <div className="header-actions">
            {activeTab === 'chat' && activeConvID && (
              <button
                id="new-conversation-btn"
                className="btn btn-secondary btn-sm"
                onClick={() => {
                  setActiveConvID(null);
                  setMessages([]);
                }}
              >
                New Conversation
              </button>
            )}
          </div>
        </header>

        {activeTab === 'chat' && (
          <Chat
            agents={agents}
            chatAgent={chatAgent}
            setChatAgent={setChatAgent}
            activeConvID={activeConvID}
            setActiveConvID={setActiveConvID}
            messages={messages}
            loadMessages={loadMessages}
            loadConversations={loadConversations}
            showToast={showToast}
          />
        )}

        {activeTab === 'conversations' && (
          <Conversations
            conversations={conversations}
            selectConversation={selectConversation}
          />
        )}

        {activeTab === 'providers' && (
          <Providers
            providers={providers}
            loadProviders={loadProviders}
            showToast={showToast}
          />
        )}

        {activeTab === 'agents' && (
          <Agents
            agents={agents}
            providers={providers}
            loadAgents={loadAgents}
            showToast={showToast}
          />
        )}

        {activeTab === 'skills' && (
          <Skills
            skills={skills}
            loadSkills={loadSkills}
            showToast={showToast}
          />
        )}

        {activeTab === 'hooks' && (
          <Hooks
            agents={agents}
            showToast={showToast}
          />
        )}

        {activeTab === 'mcp' && (
          <MCP
            showToast={showToast}
          />
        )}

        {activeTab === 'tools' && (
          <Tools
            showToast={showToast}
          />
        )}
      </main>

    </div>
  );
}
