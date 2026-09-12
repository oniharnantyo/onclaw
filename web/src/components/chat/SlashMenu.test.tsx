/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { SlashMenu } from './SlashMenu';

const menuEl = () => document.querySelector('[data-od-id="slash-menu"]')!;

// Slash menu (chat-compact-command D7): COMMANDS is exactly /compact — the
// menu lists it with its description and nothing else.
describe('components/chat/SlashMenu', () => {
  it('lists only /compact with its description', () => {
    render(<SlashMenu q="/" idx={0} onPick={vi.fn()}/>);

    expect(menuEl().textContent).toContain('/compact');
    expect(menuEl().textContent).toContain("Compact this conversation's context");
    // No leftover prototype commands anywhere in the menu.
    expect(menuEl().textContent).not.toContain('/tools');
    expect(menuEl().textContent).not.toContain('/model');
    expect(menuEl().textContent).not.toContain('/schedule');
    expect(menuEl().textContent).not.toContain('/reset');
    expect(menuEl().textContent).not.toContain('/help');
    // Exactly one entry.
    expect(menuEl().querySelectorAll('button').length).toBe(1);
  });

  it('filters by the typed prefix and hides on a non-matching query', () => {
    const { unmount } = render(<SlashMenu q="/comp" idx={0} onPick={vi.fn()}/>);
    expect(menuEl().querySelectorAll('button').length).toBe(1);
    unmount();

    render(<SlashMenu q="/foo" idx={0} onPick={vi.fn()}/>);
    expect(menuEl()).toBeNull();
  });

  it('picks the command through onPick', () => {
    const onPick = vi.fn();
    render(<SlashMenu q="/" idx={0} onPick={onPick}/>);
    fireEvent.mouseDown(menuEl().querySelector('button')!);
    expect(onPick).toHaveBeenCalledWith('/compact');
  });
});
