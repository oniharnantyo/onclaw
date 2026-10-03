import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AgentsView } from './AgentsView';
import { AgentCard } from './AgentCard';
import { useAuthStore } from '../store/auth';

describe('screens/AgentsView & AgentCard', () => {
  const mockAgents = [
    {
      id: 'a1',
      slug: 'atlas',
      name: 'Atlas',
      model: 'gpt-4o',
      temp: 0.7,
      autonomy: 'approval',
      channelPost: false,
      role: 'General assistant',
      description: 'Answers everyday questions for the team.',
      status: 'running',
      disabled_tools: ['web', 'files'],
      skills: ['research'],
      lastActive: 'just now',
      prompts_status: 'ready',
    },
    {
      id: 'a2',
      slug: 'radar',
      name: 'Radar',
      model: 'claude-3-7-sonnet',
      temp: 1.0,
      autonomy: 'full',
      channelPost: false,
      role: 'Pricing monitor',
      description: 'Watches competitor pricing pages and flags changes.',
      status: 'idle',
      disabled_tools: ['web'],
      skills: [],
      lastActive: '5m ago',
      prompts_status: 'generating',
    },
    {
      id: 'a3',
      slug: 'scout',
      name: 'Scout',
      model: 'gemini-2.5-pro',
      temp: 1.0,
      autonomy: 'suggest',
      channelPost: false,
      role: 'PR Triage',
      description: 'Triages inbound pull requests and flags risky changes.',
      status: 'idle',
      disabled_tools: [],
      skills: [],
      lastActive: '1d ago',
      prompts_status: 'failed',
      prompts_error: 'Provider rejected the API key',
    },
  ];

  const mockTenant: any = {
    id: 'acme',
    name: 'Acme Corp',
    agents: mockAgents,
  };

  it('renders avatar cards with the model once and the autonomy chip', () => {
    render(
      <AgentsView
        tenant={mockTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    expect(screen.getByText('Atlas')).not.toBeNull();
    expect(screen.getByText('Radar')).not.toBeNull();
    expect(screen.getByText('Scout')).not.toBeNull();

    // The model renders exactly once per card — the identity line; no chip duplicate.
    expect(screen.getAllByText('gpt-4o')).toHaveLength(1);
    expect(screen.getAllByText('claude-3-7-sonnet')).toHaveLength(1);
    expect(screen.getAllByText('gemini-2.5-pro')).toHaveLength(1);
    expect(screen.getByText('with approval')).not.toBeNull();
    expect(screen.getByText('autonomous')).not.toBeNull();

    // Tools, last-active, and the live dot are gone from the card.
    expect(screen.queryByText('files')).toBeNull();
    expect(screen.queryByText(/last active/)).toBeNull();
  });

  it('renders the role in the identity line and the description as the body line', () => {
    render(
      <AgentCard a={mockAgents[1] as any} onChat={vi.fn()} onConfigure={vi.fn()} />
    );
    expect(screen.getByText('Pricing monitor')).not.toBeNull();
    expect(screen.getByText('Watches competitor pricing pages and flags changes.')).not.toBeNull();
  });

  it('suppresses the role and description when they echo the name', () => {
    const echo: any = {
      id: 'a4',
      slug: 'echo',
      name: 'Echo',
      model: 'gpt-4o',
      temp: 1.0,
      autonomy: 'full',
      role: 'Echo',
      description: 'Echo',
      status: 'idle',
      disabled_tools: [],
      skills: [],
      lastActive: 'just now',
      prompts_status: 'ready',
    };
    render(<AgentCard a={echo} onChat={vi.fn()} onConfigure={vi.fn()} />);
    // The name appears once (the heading); the echoed role/description add no duplicate lines.
    expect(screen.getAllByText('Echo')).toHaveLength(1);
  });

  it('displays animated generating indicator for generating prompt status', () => {
    render(
      <AgentCard
        a={mockAgents[1] as any}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
      />
    );

    expect(screen.getByTestId('generating-indicator')).not.toBeNull();
    expect(screen.getByText(/generating prompts…/i)).not.toBeNull();
  });

  it('displays failed prompt status with error text and Retry button calling onRegenerate', () => {
    const onRegenerate = vi.fn();
    render(
      <AgentCard
        a={mockAgents[2] as any}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onRegenerate={onRegenerate}
      />
    );

    expect(screen.getByTestId('failed-indicator')).not.toBeNull();
    expect(screen.getByText('Provider rejected the API key')).not.toBeNull();

    const retryBtn = screen.getByTestId('btn-retry-generation');
    fireEvent.click(retryBtn);

    expect(onRegenerate).toHaveBeenCalledWith('a3');
  });

  it('renders empty state when workspace has 0 agents', () => {
    const emptyTenant: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: [],
    };
    const onDeploy = vi.fn();

    render(
      <AgentsView
        tenant={emptyTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={onDeploy}
      />
    );

    expect(screen.getByTestId('agents-empty')).not.toBeNull();
    expect(screen.getByText('No agents in Acme Corp yet')).not.toBeNull();

    fireEvent.click(screen.getByTestId('btn-agents-empty-deploy'));
    expect(onDeploy).toHaveBeenCalled();
  });

  it('filters roster by agent name (case-insensitive substring) and updates header count', () => {
    render(
      <AgentsView
        tenant={mockTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    const searchInput = screen.getByTestId('agents-search');
    fireEvent.change(searchInput, { target: { value: 'rad' } });

    expect(screen.getByText('Radar')).not.toBeNull();
    expect(screen.queryByText('Atlas')).toBeNull();
    expect(screen.queryByText('Scout')).toBeNull();

    expect(screen.getByText(/1 of 3 agents in Acme Corp/)).not.toBeNull();
  });

  it('does not match agents when query matches role or description but not name', () => {
    render(
      <AgentsView
        tenant={mockTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    const searchInput = screen.getByTestId('agents-search');
    // 'Pricing monitor' is the role of Radar, but not its name
    fireEvent.change(searchInput, { target: { value: 'Pricing' } });

    expect(screen.queryByText('Radar')).toBeNull();
    expect(screen.queryByText('Atlas')).toBeNull();
    expect(screen.queryByText('Scout')).toBeNull();

    expect(screen.getByTestId('agents-no-match')).not.toBeNull();
    expect(screen.getByText('No agents match "Pricing"')).not.toBeNull();
  });

  it('switches sort order between Newest first (default), Name A–Z, and Oldest', () => {
    const sortAgents = [
      { id: 'z1', name: 'Zeta', status: 'idle', disabled_tools: [], skills: [] },
      { id: 'a1', name: 'Alpha', status: 'idle', disabled_tools: [], skills: [] },
      { id: 'b1', name: 'Beta', status: 'idle', disabled_tools: [], skills: [] },
    ];
    const sortTenant: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: sortAgents,
    };

    render(
      <AgentsView
        tenant={sortTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    const getCardIds = () =>
      screen.getAllByTestId(/^agent-card-/).map((el) => el.getAttribute('data-testid'));

    // Default is newest first (store order: Zeta, Alpha, Beta)
    expect(getCardIds()).toEqual(['agent-card-z1', 'agent-card-a1', 'agent-card-b1']);

    const sortSelect = screen.getByTestId('agents-sort');

    // Switch to Name A–Z
    fireEvent.change(sortSelect, { target: { value: 'name' } });
    expect(getCardIds()).toEqual(['agent-card-a1', 'agent-card-b1', 'agent-card-z1']);

    // Switch to Oldest (reverse store order)
    fireEvent.change(sortSelect, { target: { value: 'oldest' } });
    expect(getCardIds()).toEqual(['agent-card-b1', 'agent-card-a1', 'agent-card-z1']);

    // Switch back to Newest first
    fireEvent.change(sortSelect, { target: { value: 'newest' } });
    expect(getCardIds()).toEqual(['agent-card-z1', 'agent-card-a1', 'agent-card-b1']);
  });

  it('shows no-match state when search yields no agents and clears search on button click', () => {
    render(
      <AgentsView
        tenant={mockTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    const searchInput = screen.getByTestId('agents-search');
    fireEvent.change(searchInput, { target: { value: 'Nonexistent' } });

    expect(screen.getByTestId('agents-no-match')).not.toBeNull();
    expect(screen.getByText('No agents match "Nonexistent"')).not.toBeNull();
    expect(screen.getByText(/0 of 3 agents in Acme Corp/)).not.toBeNull();

    const clearBtn = screen.getByTestId('btn-clear-search');
    fireEvent.click(clearBtn);

    expect(screen.queryByTestId('agents-no-match')).toBeNull();
    expect(screen.getByText('Atlas')).not.toBeNull();
    expect(screen.getByText('Radar')).not.toBeNull();
    expect(screen.getByText('Scout')).not.toBeNull();
    expect((searchInput as HTMLInputElement).value).toBe('');
    expect(screen.getByText(/3 agents in Acme Corp/)).not.toBeNull();
  });

  const createManyAgents = (count: number): any[] =>
    Array.from({ length: count }, (_, i) => {
      const num = String(i + 1).padStart(2, '0');
      return {
        id: `agent-${num}`,
        slug: `agent-${num}`,
        name: `Agent ${num}`,
        model: 'gpt-4o',
        temp: 0.7,
        autonomy: 'approval',
        role: `Role ${num}`,
        status: 'idle',
        disabled_tools: [],
        skills: [],
        lastActive: 'just now',
      };
    });

  it('slices agents into 24 cards per page and supports bound-disabled pagination', () => {
    const fiftyAgents = createManyAgents(50);
    const manyTenant: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: fiftyAgents,
    };

    render(
      <AgentsView
        tenant={manyTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    // Page 1
    const pager = screen.getByTestId('agents-pager');
    expect(pager).not.toBeNull();
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 1 of 3');
    expect(screen.getByTestId('agent-card-agent-01')).not.toBeNull();
    expect(screen.getByTestId('agent-card-agent-24')).not.toBeNull();
    expect(screen.queryByTestId('agent-card-agent-25')).toBeNull();

    const prevBtn = screen.getByTestId('agents-pager-prev') as HTMLButtonElement;
    const nextBtn = screen.getByTestId('agents-pager-next') as HTMLButtonElement;
    expect(prevBtn.disabled).toBe(true);
    expect(nextBtn.disabled).toBe(false);

    // Navigate to Page 2
    fireEvent.click(nextBtn);
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 2 of 3');
    expect(screen.queryByTestId('agent-card-agent-24')).toBeNull();
    expect(screen.getByTestId('agent-card-agent-25')).not.toBeNull();
    expect(screen.getByTestId('agent-card-agent-48')).not.toBeNull();
    expect(screen.queryByTestId('agent-card-agent-49')).toBeNull();
    expect(prevBtn.disabled).toBe(false);
    expect(nextBtn.disabled).toBe(false);

    // Navigate to Page 3
    fireEvent.click(nextBtn);
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 3 of 3');
    expect(screen.queryByTestId('agent-card-agent-48')).toBeNull();
    expect(screen.getByTestId('agent-card-agent-49')).not.toBeNull();
    expect(screen.getByTestId('agent-card-agent-50')).not.toBeNull();
    expect(prevBtn.disabled).toBe(false);
    expect(nextBtn.disabled).toBe(true);

    // Navigate back to Page 2
    fireEvent.click(prevBtn);
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 2 of 3');
    expect(screen.getByTestId('agent-card-agent-25')).not.toBeNull();
  });

  it('hides pager footer entirely when matched agents count is <= 24', () => {
    const twentyFourAgents = createManyAgents(24);
    const tenant24: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: twentyFourAgents,
    };

    const { rerender } = render(
      <AgentsView
        tenant={tenant24}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    expect(screen.queryByTestId('agents-pager')).toBeNull();

    // With 25 agents, pager appears
    const twentyFiveAgents = createManyAgents(25);
    rerender(
      <AgentsView
        tenant={{ id: 'acme', name: 'Acme Corp', agents: twentyFiveAgents } as any}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );
    expect(screen.getByTestId('agents-pager')).not.toBeNull();
  });

  it('resets page to 1 when search or sort changes', () => {
    const fiftyAgents = createManyAgents(50);
    const manyTenant: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: fiftyAgents,
    };

    render(
      <AgentsView
        tenant={manyTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    // Go to page 2
    fireEvent.click(screen.getByTestId('agents-pager-next'));
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 2 of 3');

    // Type in search -> resets to page 1
    fireEvent.change(screen.getByTestId('agents-search'), { target: { value: 'Agent' } });
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 1 of 3');

    // Go to page 2 again
    fireEvent.click(screen.getByTestId('agents-pager-next'));
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 2 of 3');

    // Change sort -> resets to page 1
    fireEvent.change(screen.getByTestId('agents-sort'), { target: { value: 'name' } });
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 1 of 3');
  });

  it('clamps rendered page to the last page when results shrink', () => {
    const fiftyAgents = createManyAgents(50);
    const manyTenant: any = {
      id: 'acme',
      name: 'Acme Corp',
      agents: fiftyAgents,
    };

    const { rerender } = render(
      <AgentsView
        tenant={manyTenant}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    // Navigate to page 3
    fireEvent.click(screen.getByTestId('agents-pager-next'));
    fireEvent.click(screen.getByTestId('agents-pager-next'));
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 3 of 3');
    expect(screen.getByTestId('agent-card-agent-50')).not.toBeNull();

    // Rerender with only 30 agents (2 pages total)
    rerender(
      <AgentsView
        tenant={{ id: 'acme', name: 'Acme Corp', agents: fiftyAgents.slice(0, 30) } as any}
        onChat={vi.fn()}
        onConfigure={vi.fn()}
        onDeploy={vi.fn()}
      />
    );

    // Rendered page clamps to page 2
    expect(screen.getByTestId('agents-pager-label').textContent).toContain('Page 2 of 2');
    expect(screen.getByTestId('agent-card-agent-25')).not.toBeNull();
    expect(screen.getByTestId('agent-card-agent-30')).not.toBeNull();
    expect(screen.queryByTestId('agent-card-agent-01')).toBeNull();
  });
});

describe('screens/AgentsView — agents.write deploy gating (fix-role-permission-audit 5.5)', () => {
  const agent = {
    id: 'a1', slug: 'atlas', name: 'Atlas', model: 'gpt-4o', temp: 0.7, autonomy: 'approval',
    channelPost: false, role: 'Assistant', description: 'Helps.', status: 'idle',
    disabled_tools: [], skills: [], lastActive: '', prompts_status: 'ready',
  };
  const tenant: any = { id: 'acme', name: 'Acme Corp', agents: [agent] };

  it('hides the deploy controls from a Member without agents.write but keeps the roster', () => {
    // Built-in Member set per the decided catalog: reads + channels.*.
    useAuthStore.setState({
      user: { id: 'u1', email: 'u1@acme.dev', name: 'U One', created_at: '', updated_at: '' },
      memberships: [
        {
          workspace_id: 'acme',
          role_name: 'Member',
          role: {
            name: 'Member',
            is_owner: false,
            permissions: [
              'workspace.read',
              'members.read',
              'roles.read',
              'providers.read',
              'agents.read',
              'skills.read',
              'scheduler.read',
              'channels.read',
              'channels.write',
            ],
          },
        },
      ] as any,
      status: 'authenticated',
    });

    render(<AgentsView tenant={tenant} onChat={vi.fn()} onConfigure={vi.fn()} onDeploy={vi.fn()} />);

    expect(screen.getByTestId('agent-card-a1')).not.toBeNull();
    expect(screen.queryByTestId('btn-deploy-agent')).toBeNull();
    expect(screen.queryByTestId('btn-agents-empty-deploy')).toBeNull();
  });

  it('keeps the deploy controls in mock mode (no memberships)', () => {
    useAuthStore.setState({ user: null, memberships: [], status: 'authenticated' } as any);

    render(<AgentsView tenant={tenant} onChat={vi.fn()} onConfigure={vi.fn()} onDeploy={vi.fn()} />);

    expect(screen.queryByTestId('btn-deploy-agent')).not.toBeNull();
  });
});
