import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { Tooltip } from './Tooltip';

describe('components/ui/Tooltip', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('renders child element and keeps tooltip hidden initially', () => {
    render(
      <Tooltip content="Helpful info">
        <button type="button">Hover me</button>
      </Tooltip>
    );

    expect(screen.getByRole('button', { name: 'Hover me' })).not.toBeNull();
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('shows tooltip after 150ms hover delay and sets aria-describedby', () => {
    render(
      <Tooltip content="Workspaces" id="ws-tooltip">
        <button type="button">WS</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'WS' });
    expect(btn.getAttribute('aria-describedby')).toBeNull();

    fireEvent.mouseEnter(btn);
    // Not visible immediately
    expect(screen.queryByRole('tooltip')).toBeNull();

    // Advance 149ms - still not visible
    act(() => {
      vi.advanceTimersByTime(149);
    });
    expect(screen.queryByRole('tooltip')).toBeNull();

    // Advance 1ms - visible at 150ms
    act(() => {
      vi.advanceTimersByTime(1);
    });
    const tooltip = screen.getByRole('tooltip');
    expect(tooltip).not.toBeNull();
    expect(tooltip.textContent).toBe('Workspaces');
    expect(tooltip.id).toBe('ws-tooltip');
    expect(btn.getAttribute('aria-describedby')).toBe('ws-tooltip');
  });

  it('hides tooltip on mouse leave and clears pending timer', () => {
    render(
      <Tooltip content="Workspaces">
        <button type="button">WS</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'WS' });

    // Start hover then leave before delay
    fireEvent.mouseEnter(btn);
    act(() => {
      vi.advanceTimersByTime(50);
    });
    fireEvent.mouseLeave(btn);
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.queryByRole('tooltip')).toBeNull();

    // Hover again, let it open, then leave
    fireEvent.mouseEnter(btn);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.getByRole('tooltip')).not.toBeNull();

    fireEvent.mouseLeave(btn);
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('shows tooltip on focus and hides on blur', () => {
    render(
      <Tooltip content="Settings">
        <button type="button">Settings</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'Settings' });

    fireEvent.focus(btn);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.getByRole('tooltip').textContent).toBe('Settings');

    fireEvent.blur(btn);
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('dismisses tooltip on Escape key press on trigger', () => {
    render(
      <Tooltip content="Settings">
        <button type="button">Settings</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'Settings' });
    fireEvent.focus(btn);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.getByRole('tooltip')).not.toBeNull();

    fireEvent.keyDown(btn, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('dismisses tooltip on global window Escape key press', () => {
    render(
      <Tooltip content="Global Escape Test">
        <button type="button">Trigger</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'Trigger' });
    fireEvent.mouseEnter(btn);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.getByRole('tooltip')).not.toBeNull();

    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('applies custom placement classes', () => {
    const { rerender } = render(
      <Tooltip content="Top tip" placement="top">
        <button type="button">Btn</button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'Btn' });
    fireEvent.focus(btn);
    act(() => {
      vi.advanceTimersByTime(150);
    });
    expect(screen.getByRole('tooltip').className).toContain('bottom-[calc(100%+8px)]');

    rerender(
      <Tooltip content="Left tip" placement="left">
        <button type="button">Btn</button>
      </Tooltip>
    );
    expect(screen.getByRole('tooltip').className).toContain('right-[calc(100%+8px)]');
  });

  it('preserves existing child event handlers', () => {
    const onClick = vi.fn();
    const onMouseEnter = vi.fn();

    render(
      <Tooltip content="Info">
        <button type="button" onClick={onClick} onMouseEnter={onMouseEnter}>
          Clickable
        </button>
      </Tooltip>
    );

    const btn = screen.getByRole('button', { name: 'Clickable' });
    fireEvent.mouseEnter(btn);
    expect(onMouseEnter).toHaveBeenCalledTimes(1);

    fireEvent.click(btn);
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('does not show tooltip when disabled or empty content', () => {
    const { rerender } = render(
      <Tooltip content="" disabled={false}>
        <button type="button">Disabled content</button>
      </Tooltip>
    );

    const btn1 = screen.getByRole('button', { name: 'Disabled content' });
    fireEvent.mouseEnter(btn1);
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.queryByRole('tooltip')).toBeNull();

    rerender(
      <Tooltip content="Info" disabled={true}>
        <button type="button">Disabled flag</button>
      </Tooltip>
    );

    const btn2 = screen.getByRole('button', { name: 'Disabled flag' });
    fireEvent.mouseEnter(btn2);
    act(() => {
      vi.advanceTimersByTime(200);
    });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });
});
