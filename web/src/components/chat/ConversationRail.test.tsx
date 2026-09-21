/**
 * Conversation rail (elements-conversation-map wiring): the minimap derives
 * active/visible/selection state from the transcript DOM. jsdom gives every
 * element a zero-size rect, so the tests stub getBoundingClientRect to model
 * a viewport and assert the derivation, the jump, and the hide-when-short rule.
 */
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { useRef } from 'react';
import { ConversationRail } from './ConversationRail';

const entries = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ id: 'm' + (i + 1), author: 'agent', text: 'message ' + (i + 1) }));

function Harness({ msgs }: { msgs: any[] }) {
  const listRef = useRef<HTMLDivElement | null>(null);
  return (
    <div ref={listRef} data-testid="list" style={{ overflowY: 'auto' }}>
      {msgs.map((m) => (
        <div key={m.id} data-msg-id={m.id} style={{ height: 40 }}>{m.text}</div>
      ))}
      <ConversationRail entries={msgs} listRef={listRef} />
    </div>
  );
}

// Model a viewport: the list is 0..200 tall at y=0; message i occupies
// [i*40 - scrollTop, (i+1)*40 - scrollTop].
function stubRects(scrollTop = 0) {
  const list = document.querySelector('[data-testid="list"]') as HTMLElement;
  list.getBoundingClientRect = () => ({ top: 0, bottom: 200, left: 0, right: 300, height: 200, width: 300 }) as any;
  document.querySelectorAll('[data-msg-id]').forEach((el, i) => {
    (el as HTMLElement).getBoundingClientRect = () =>
      ({ top: i * 40 - scrollTop, bottom: (i + 1) * 40 - scrollTop, left: 0, right: 300, height: 40, width: 300 }) as any;
  });
}

describe('ConversationRail', () => {
  it('renders one tick per entry once there are at least two', () => {
    const { container } = render(<Harness msgs={entries(5)}/>);
    stubRects();
    expect(container.querySelectorAll('[data-slot="conversation-map-tick"]')).toHaveLength(5);
  });

  it('hides entirely for a single-entry (or empty) transcript', () => {
    const { container } = render(<Harness msgs={entries(1)}/>);
    expect(container.querySelector('[data-od-id="conversation-rail"]')).toBeNull();
  });

  it('marks the nearest-to-top on-screen entry as active and the visible set as in-view', () => {
    const msgs = entries(8);
    const { container } = render(<Harness msgs={msgs}/>);
    // Scrolled so messages 3..8 are on screen (msg1/2 above the fold).
    stubRects(80);
    const list = container.querySelector('[data-testid="list"]') as HTMLElement;
    fireEvent.scroll(list);
    return waitFor(() => {
      const ticks = container.querySelectorAll('[data-slot="conversation-map-tick"]');
      // msg3 is the first fully on-screen entry (top exactly at the fold).
      expect(ticks[2].getAttribute('data-active')).toBe('');
      expect(ticks[0].getAttribute('data-in-view')).toBeNull();
      expect(ticks[2].getAttribute('data-in-view')).toBe('');
      // msg7 (160..200) is the last visible one; msg8 starts exactly at the
      // fold's bottom edge (200 < 200 is false) — not in view.
      expect(ticks[6].getAttribute('data-in-view')).toBe('');
      expect(ticks[7].getAttribute('data-in-view')).toBeNull();
    });
  });

  it('jumps to the message on tick click', async () => {
    const msgs = entries(4);
    const { container } = render(<Harness msgs={msgs}/>);
    stubRects();
    const target = container.querySelector('[data-msg-id="m3"]') as HTMLElement;
    const spy = vi.fn();
    target.scrollIntoView = spy;
    const ticks = container.querySelectorAll('[data-slot="conversation-map-tick"]');
    fireEvent.click(ticks[2]);
    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(spy.mock.calls[0][0]).toMatchObject({ block: 'start' });
  });

  it('labels ticks for the hover preview: text digest, else a kind label', () => {
    const msgs = [
      { id: 'm1', author: 'you', text: 'ping the API' },
      { id: 'm2', author: 'agent', text: '' },
      { id: 'm3', author: 'notice', text: '', notice: { hook: 'Compliance Gate', reason: 'no' } },
    ];
    const { container } = render(<Harness msgs={msgs}/>);
    stubRects();
    const ticks = container.querySelectorAll('[data-slot="conversation-map-tick"]');
    expect(ticks[0].getAttribute('aria-label')).toBe('ping the API');
    expect(ticks[1].getAttribute('aria-label')).toBe('Agent');
    expect(ticks[2].getAttribute('aria-label')).toBe('Prompt blocked');
  });
});
