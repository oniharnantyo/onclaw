import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AttachAgentsList } from './AttachAgentsList';
import type { ApiAgent } from '../../lib/api';

// Minimal ApiAgent factory — the component reads only id/name and hands the
// whole agent back to the caller's isAttached/onToggle.
const agent = (overrides: Partial<ApiAgent> = {}): ApiAgent => ({
  id: 'a-1',
  workspace_id: 'acme',
  slug: 'atlas',
  name: 'Atlas',
  role: '',
  description: '',
  brief: '',
  identity: '',
  soul: '',
  bootstrap: '',
  provider_id: 'openai',
  model: 'gpt-4o',
  temperature: 0.7,
  autonomy: 'full',
  tools: [],
  enabled_mcps: [],
  avatar: {},
  prompts_status: 'ready',
  created_at: '',
  updated_at: '',
  ...overrides,
});

describe('components/connections/AttachAgentsList', () => {
  const agents = [
    agent(),
    agent({ id: 'a-2', slug: 'beacon', name: 'Beacon', enabled_mcps: ['srv-1'] }),
    agent({ id: 'a-3', slug: '', name: 'Cronus' }),
  ];

  const renderList = (
    props: Partial<Parameters<typeof AttachAgentsList>[0]> = {}
  ) =>
    render(
      <AttachAgentsList
        agents={agents}
        isAttached={(a) => (a.enabled_mcps ?? []).includes('srv-1')}
        onToggle={vi.fn()}
        {...props}
      />
    );

  const openList = () => {
    fireEvent.click(screen.getByTestId('attach-agents-trigger'));
  };

  it('renders the trigger summarising the attached agents (single label)', () => {
    renderList();

    expect(screen.getByTestId('attach-agents-list')).toBeTruthy();
    const trigger = screen.getByRole('combobox', { name: 'Attached agents' });
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    // Only Beacon is attached, so the summary is its label alone.
    expect(trigger.textContent).toContain('Beacon');
    expect(trigger.textContent).not.toContain('Atlas');
    expect(trigger.textContent).not.toContain('+');
  });

  it('lists every agent when opened, marking attached ones selected', () => {
    renderList();
    openList();

    expect(screen.getByRole('combobox').getAttribute('aria-expanded')).toBe('true');
    expect(screen.getAllByRole('option').length).toBe(agents.length);
    // Beacon is attached; Atlas and Cronus (empty slug, real id) are not.
    expect(screen.getByTestId('multiselect-option-a-2').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('multiselect-option-a-1').getAttribute('aria-selected')).toBe('false');
    expect(screen.getByTestId('multiselect-option-a-3').getAttribute('aria-selected')).toBe('false');
  });

  it('filters the rows as the search query is typed', () => {
    renderList();
    openList();

    fireEvent.change(screen.getByTestId('multiselect-search-input'), {
      target: { value: 'Bea' },
    });

    expect(screen.getAllByRole('option').length).toBe(1);
    expect(screen.getByTestId('multiselect-option-a-2')).toBeTruthy();
    expect(screen.queryByTestId('multiselect-option-a-1')).toBeNull();

    fireEvent.change(screen.getByTestId('multiselect-search-input'), {
      target: { value: 'nobody' },
    });

    expect(screen.queryAllByRole('option').length).toBe(0);
    expect(screen.getByText('No agents match.')).toBeTruthy();
  });

  it('calls onToggle with the agent when a row is toggled on', () => {
    const onToggle = vi.fn();
    renderList({ onToggle });
    openList();

    fireEvent.click(screen.getByTestId('multiselect-option-a-1'));

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onToggle).toHaveBeenCalledWith(agents[0]);
  });

  it('keeps the popover open after a row is toggled', () => {
    renderList();
    openList();

    fireEvent.click(screen.getByTestId('multiselect-option-a-1'));

    // The defining behaviour of the picker: several agents in one pass.
    expect(screen.getByTestId('attach-agents-popover')).toBeTruthy();
    expect(screen.getByTestId('multiselect-search-input')).toBeTruthy();
    expect(screen.getAllByRole('option').length).toBe(agents.length);
  });

  it('calls onToggle with the agent when an attached row is toggled off', () => {
    const onToggle = vi.fn();
    renderList({ onToggle });
    openList();

    fireEvent.click(screen.getByTestId('multiselect-option-a-2'));

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onToggle).toHaveBeenCalledWith(agents[1]);
  });

  it('emits one onToggle per changed agent, in agents order, across one open pass', () => {
    // Mirrors the real callers: they hold attachment state and re-render the
    // list, so `isAttached` is fresh for every click.
    const onToggle = vi.fn();
    function Harness() {
      const [attached, setAttached] = React.useState<string[]>(['a-2']);
      return (
        <AttachAgentsList
          agents={agents}
          isAttached={(a) => attached.includes(a.id)}
          onToggle={(a) => {
            onToggle(a);
            setAttached((prev) =>
              prev.includes(a.id) ? prev.filter((id) => id !== a.id) : [...prev, a.id]
            );
          }}
        />
      );
    }
    render(<Harness />);
    openList();

    // Tick Atlas and Cronus, then untick Beacon — all without reopening.
    fireEvent.click(screen.getByTestId('multiselect-option-a-1'));
    fireEvent.click(screen.getByTestId('multiselect-option-a-3'));
    fireEvent.click(screen.getByTestId('multiselect-option-a-2'));

    expect(onToggle.mock.calls.map(([a]) => a)).toEqual([agents[0], agents[2], agents[1]]);
    expect(screen.getByTestId('multiselect-option-a-1').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('multiselect-option-a-2').getAttribute('aria-selected')).toBe('false');
    expect(screen.getByTestId('multiselect-option-a-3').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('attach-agents-popover')).toBeTruthy();
  });

  it('blocks toggling while disabled', () => {
    const onToggle = vi.fn();
    renderList({ onToggle, disabled: true });

    const trigger = screen.getByTestId('attach-agents-trigger') as HTMLButtonElement;
    expect(trigger.disabled).toBe(true);

    fireEvent.click(trigger);

    expect(screen.queryByTestId('attach-agents-popover')).toBeNull();
    expect(screen.queryAllByRole('option').length).toBe(0);
    expect(onToggle).not.toHaveBeenCalled();
  });
});
