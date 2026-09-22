/**
 * @vitest-environment jsdom
 */
// Members source tests (add-right-panel 1.5): ContextPanel's body lives on as
// a registered panel source with its behavior intact — listing, presence and
// kind markers, remove eligibility (the channel's primary agent is not
// removable), and the add-member flow. Registration is a module-load side
// effect of importing ./members, exactly like the built-in generative-UI
// renderers — so this suite imports it and never resets the registries.
import { describe, it, expect } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import { renderPanelSource } from '../../../../lib/panel/registry';
import './members';

const tab = { id: 'tab1', kind: 'members', title: 'Members', payload: { chatId: 'ch-ops' }, dedupKey: 'members::{}' };

const ctx = {
  channelMembers: [
    { id: 'a1', kind: 'agent', name: 'Atlas', agent: { status: 'idle' } },
    { id: 'p1', kind: 'person', name: 'Alice', presence: 'online' },
    { id: 'p2', kind: 'person', name: 'Bob', presence: 'away' },
  ],
  memberCandidates: [{ id: 'p3', kind: 'person', name: 'Cara', presence: 'online' }],
  primaryAgentId: 'a1',
  onAddMember: (id: string) => void id,
  onRemoveMember: (id: string) => void id,
  onOpenMember: (id: string) => void id,
};

const renderMembers = (over: Record<string, unknown> = {}) =>
  render(<div>{renderPanelSource('members', { tab, ctx: { ...ctx, ...over } })}</div>);

describe('panel sources/members — registered source behavior (1.5)', () => {
  it('is registered under the members kind', () => {
    expect(renderPanelSource('members', { tab, ctx: {} })).not.toBeNull();
  });

  it('renders member rows with kind and presence markers', () => {
    const { container } = renderMembers();
    expect(container.textContent).toContain('Atlas');
    expect(container.textContent).toContain('Agent');
    expect(container.textContent).toContain('Member · online');
    expect(container.textContent).toContain('Member · away');
  });

  it('remove eligibility: the primary agent is not removable, others are', () => {
    const { container } = renderMembers();
    expect(container.querySelector('[data-od-id="drawer-member-remove-a1"]')).toBeNull();
    expect(container.querySelector('[data-od-id="drawer-member-remove-p1"]')).not.toBeNull();
  });

  it('the add-member flow lists eligible candidates and empties out when none remain', () => {
    const { container } = renderMembers();
    fireEvent.click(container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    expect(container.querySelector('[data-od-id="add-member-list"]')).not.toBeNull();
    expect(container.querySelector('[data-od-id="add-member-p3"]')).not.toBeNull();

    const empty = renderMembers({ memberCandidates: [] });
    fireEvent.click(empty.container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    expect(empty.container.textContent).toContain('Everyone here is already a member.');
  });

  it('an empty channel says so instead of rendering a blank pane', () => {
    const { container } = renderMembers({ channelMembers: [] });
    expect(container.textContent).toContain('No members yet.');
  });

  it('clicking add and remove routes through the context callbacks', () => {
    const added: string[] = [];
    const removed: string[] = [];
    const { container } = renderMembers({
      onAddMember: (id: string) => added.push(id),
      onRemoveMember: (id: string) => removed.push(id),
    });
    fireEvent.click(container.querySelector('[data-od-id="drawer-member-remove-p2"]') as HTMLButtonElement);
    fireEvent.click(container.querySelector('[data-od-id="btn-add-member"]') as HTMLButtonElement);
    fireEvent.click(container.querySelector('[data-od-id="add-member-p3"]') as HTMLButtonElement);
    expect(removed).toEqual(['p2']);
    expect(added).toEqual(['p3']);
  });
});
