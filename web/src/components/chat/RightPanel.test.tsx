/**
 * @vitest-environment jsdom
 */
// RightPanel shell tests (add-right-panel 1.2): tab strip mechanics, source
// dispatch through the registry, and the chrome contract — docked ≥xl plus
// overlay sheet <xl, the exact pattern ContextPanel established in ChatRoute.
// The chrome renders BOTH instances (docked + overlay) as the old pattern
// did, so queries scope to the docked one.
import { describe, it, expect, beforeEach } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { RightPanel } from './RightPanel';
import { useStore } from '../../store';
import { registerPanelSource, resetPanelRegistries } from '../../lib/panel/registry';

beforeEach(() => {
  resetPanelRegistries();
  useStore.setState({ panel: { open: false, tabs: [], activeId: null, badge: false } } as any);
});

const openTab = (title: string, path: string) =>
  useStore.getState().openPanelTab({ kind: 'fake', title, payload: { path } });

const dockedOf = (container: HTMLElement) =>
  container.querySelector('[data-od-id="right-panel-docked"]') as HTMLElement;

describe('components/chat/RightPanel — shell', () => {
  it('renders nothing while the panel is closed', () => {
    const { container } = render(<RightPanel/>);
    expect(container.querySelector('[data-od-id="right-panel"]')).toBeNull();
    expect(container.querySelector('[data-od-id="right-panel-overlay"]')).toBeNull();
  });

  it('open panel renders the docked column and the <xl overlay sheet', () => {
    openTab('brief.md', 'docs/brief.md');
    const { container } = render(<RightPanel/>);
    expect(dockedOf(container).className).toContain('hidden xl:flex');
    const docked = container.querySelector('[data-od-id="right-panel"]');
    expect(docked?.className).toContain('w-[400px]');
    expect(docked?.className).toContain('flex-col');
    const overlay = container.querySelector('[data-od-id="right-panel-overlay"]');
    expect(overlay).not.toBeNull();
    expect(overlay?.className).toContain('fixed inset-0 z-40');
    expect(overlay?.className).toContain('xl:hidden');
    // Dismissal backdrop.
    expect(overlay?.querySelector('.od-fade')).not.toBeNull();
  });

  it('the tab strip shows titles, marks the active tab, and dispatches content through the registry', () => {
    registerPanelSource('fake', ({ tab }) => <p data-od-id="fake-source">content of {tab.payload.path as string}</p>);
    openTab('brief.md', 'docs/brief.md');
    openTab('notes.md', 'docs/notes.md');
    const { container } = render(<RightPanel/>);
    const docked = dockedOf(container);
    const tabs = docked.querySelectorAll('[role="tab"]');
    expect(tabs).toHaveLength(2);
    expect(tabs[0].textContent).toBe('brief.md');
    expect(tabs[1].getAttribute('aria-selected')).toBe('true'); // last opened is active
    expect(docked.querySelector('[data-od-id="fake-source"]')?.textContent).toBe('content of docs/notes.md');
    // Focusing a tab swaps the dispatched content.
    fireEvent.click(tabs[0]);
    expect(useStore.getState().panel.activeId).toBe(useStore.getState().panel.tabs[0].id);
    expect(docked.querySelector('[data-od-id="fake-source"]')?.textContent).toBe('content of docs/brief.md');
  });

  it('per-tab close removes that tab; closing the last one closes the panel', () => {
    registerPanelSource('fake', () => null);
    openTab('a.md', 'a.md');
    openTab('b.md', 'b.md');
    const { container } = render(<RightPanel/>);
    const docked = dockedOf(container);
    // Both tabs share a kind — one close control per TAB.
    expect(docked.querySelectorAll('[data-od-id="panel-tab-close-fake"]')).toHaveLength(2);
    fireEvent.click(docked.querySelectorAll('[data-od-id="panel-tab-close-fake"]')[1]);
    expect(useStore.getState().panel.tabs).toHaveLength(1);
    expect(useStore.getState().panel.open).toBe(true);
    fireEvent.click(docked.querySelector('[data-od-id="panel-tab-close-fake"]') as HTMLElement);
    expect(useStore.getState().panel.open).toBe(false);
    expect(useStore.getState().panel.tabs).toHaveLength(0);
  });

  it('the panel close control and the overlay backdrop hide the panel (tabs kept)', () => {
    registerPanelSource('fake', () => null);
    openTab('a.md', 'a.md');
    const { container } = render(<RightPanel/>);
    fireEvent.click(dockedOf(container).querySelector('[data-od-id="btn-panel-close"]') as HTMLElement);
    expect(useStore.getState().panel.open).toBe(false);
    expect(useStore.getState().panel.tabs).toHaveLength(1);
    // Reopen — the overlay's dismissal backdrop closes it too.
    act(() => { useStore.getState().setPanelOpen(true); });
    fireEvent.click(container.querySelector('.od-fade') as HTMLElement);
    expect(useStore.getState().panel.open).toBe(false);
  });

  it('an open panel with no tabs shows the terse empty state; an unregistered kind shows the fallback', () => {
    const { container } = render(<RightPanel/>);
    act(() => { useStore.getState().setPanelOpen(true); });
    expect(dockedOf(container).querySelector('[data-od-id="panel-empty"]')).not.toBeNull();
    expect(dockedOf(container).textContent).toContain('Nothing open');

    act(() => { openTab('mystery', 'x'); });
    expect(dockedOf(container).querySelector('[data-od-id="panel-source-missing"]')).not.toBeNull();
    expect(dockedOf(container).textContent).toContain('This source is not available.');
  });

  it('renders a resize handle and allows drag-resizing within bounds', () => {
    openTab('brief.md', 'docs/brief.md');
    const { container } = render(<RightPanel/>);
    const docked = dockedOf(container);
    const handle = docked.querySelector('[data-od-id="panel-resize-handle"]') as HTMLElement;
    expect(handle).not.toBeNull();
    expect(handle.getAttribute('role')).toBe('separator');

    const aside = docked.querySelector('[data-od-id="right-panel"]') as HTMLElement;
    expect(aside.style.width).toBe('400px');

    // Simulate drag resize: start at clientX=1000, drag to clientX=900 (deltaX=100 => +100px width)
    fireEvent.mouseDown(handle, { clientX: 1000 });
    act(() => {
      window.dispatchEvent(new MouseEvent('mousemove', { clientX: 900 }));
    });
    expect(aside.style.width).toBe('500px');

    // Finish drag
    act(() => {
      window.dispatchEvent(new MouseEvent('mouseup'));
    });
    expect(aside.style.width).toBe('500px');

    // Double-click resets to default 400px
    fireEvent.doubleClick(handle);
    expect(aside.style.width).toBe('400px');
  });
});
