import { useState, useEffect } from 'react';
import {
  Terminal,
  ChatCircle,
  Key,
  Robot,
  SignOut,
  WarningCircle,
  CheckCircle,
  Code,
  Lightning,
  Brain,
  Plug,
  Wrench,
} from '@phosphor-icons/react';

import Login from './components/Login';
import Providers from './components/Providers';
import type { Provider } from './components/Providers';
import Agents from './components/Agents';
import type { Agent } from './components/Agents';
import Chat from './components/Chat';
import Skills from './components/Skills';
import type { Skill } from './components/Skills';
import ThreadRuntime from './components/ThreadRuntime';
import type { Conversation } from './types/chat';
import Hooks from './components/Hooks';
import MCP from './components/MCP';
import Tools from './components/Tools';
import Memory from './components/Memory';

type Tab = 'chat' | 'providers' | 'agents' | 'skills' | 'hooks' | 'mcp' | 'tools' | 'memory';

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

  const [providers, setProviders] = useState<Provider[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [chatAgent, setChatAgent] = useState('');

  const [threadKey, setThreadKey] = useState(0);

  const showToast = (message: string, type: 'success' | 'error' = 'success') => {
    const id = Date.now();
    setToasts((prev) => [...prev, { id, message, type }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id));
    }, 4000);
  };

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

  const handleNewConversation = () => {
    setThreadKey((k) => k + 1);
  };

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
        { id: 'chat', label: 'Live Chat', icon: <ChatCircle weight="duotone" size={18} /> },
      ],
    },
    {
      label: 'Agent',
      items: [
        { id: 'agents', label: 'Agents', icon: <Robot weight="duotone" size={18} /> },
        { id: 'skills', label: 'Skills', icon: <Code weight="duotone" size={18} /> },
        { id: 'hooks', label: 'Hooks', icon: <Lightning weight="duotone" size={18} /> },
        { id: 'memory', label: 'Memory', icon: <Brain weight="duotone" size={18} /> },
        { id: 'mcp', label: 'MCP Servers', icon: <Plug weight="duotone" size={18} /> },
        { id: 'tools', label: 'Tools', icon: <Wrench weight="duotone" size={18} /> },
      ],
    },
    {
      label: 'Configuration',
      items: [
        { id: 'providers', label: 'Providers', icon: <Key weight="duotone" size={18} /> },
      ],
    },
  ];

  const HEADER_TITLES: Record<Tab, string> = {
    chat: 'Live Agent Session',
    providers: 'LLM Providers',
    agents: 'AI Agents',
    skills: 'Agent Skills',
    hooks: 'Lifecycle Hooks',
    memory: 'Agent Memory',
    mcp: 'MCP Servers',
    tools: 'Builtin Tools',
  };

  return (
    <div className="app-container">
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

      <main className="main-content" role="main">
        <header className="main-header">
          <div className="header-title">{HEADER_TITLES[activeTab]}</div>
          <div className="header-actions">
            {activeTab === 'chat' && (
              <button
                id="new-conversation-btn"
                className="btn btn-secondary btn-sm"
                onClick={handleNewConversation}
              >
                New Conversation
              </button>
            )}
          </div>
        </header>

        {activeTab === 'chat' && (
          <ThreadRuntime
            key={threadKey}
            showToast={showToast}
            initialAgents={agents}
            initialSkills={skills}
            initialConversations={conversations}
            defaultAgent={chatAgent}
          >
            <Chat onNewConversation={handleNewConversation} />
          </ThreadRuntime>
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

        {activeTab === 'memory' && (
          <Memory
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
