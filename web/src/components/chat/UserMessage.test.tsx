/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { UserMessage } from './UserMessage';

// Transcript attachment rendering (add-chat-attachments galleries H/I/J +
// spec "Transcript attachment rendering"): images as inline thumbnails that
// open the capability URL in a new tab, documents as icon chips with
// "<TYPE> · <size>" and a download link. The same component serves the
// optimistic entry and the hydrated one — there is only one markup path.
describe('components/chat/UserMessage — attachments', () => {
  const image = { id: 'att-1', name: 'shot.png', mime: 'image/png', size: 12, url: '/api/v1/files/k1' };
  const pdf = { id: 'att-2', name: 'report.pdf', mime: 'application/pdf', size: 5033164, url: '/api/v1/files/k2' };

  it('renders an inline image thumbnail that opens the capability URL in a new tab', () => {
    render(<UserMessage m={{ id: 'm1', author: 'you', ts: '9:14', text: "Here's the shot", attachments: [image] }} members={[]}/>);

    const img = screen.getByAltText('shot.png');
    expect(img).toBeTruthy();
    expect(img.getAttribute('src')).toBe('/api/v1/files/k1');
    // Click-through: new tab, no opener leak.
    const link = img.closest('a')!;
    expect(link.getAttribute('href')).toBe('/api/v1/files/k1');
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toContain('noreferrer');
    // The message text still renders above the chip.
    expect(screen.getByText("Here's the shot")).toBeTruthy();
  });

  it('renders a document chip with "PDF · 4.8 MB" and a download href', () => {
    render(<UserMessage m={{ id: 'm2', author: 'you', ts: '9:15', text: 'Review this', attachments: [pdf] }} members={[]}/>);

    expect(screen.getByText('report.pdf')).toBeTruthy();
    expect(screen.getByText('PDF · 4.8 MB')).toBeTruthy();
    const dl = screen.getByLabelText('Download report.pdf');
    expect(dl.getAttribute('href')).toBe('/api/v1/files/k2');
    expect(dl.getAttribute('download')).toBe('report.pdf');
  });

  it('renders an attachment-only entry as chips with no text row', () => {
    const { container } = render(
      <UserMessage m={{ id: 'm3', author: 'you', ts: '9:16', text: '', attachments: [image, pdf] }} members={[]}/>
    );

    const bubble = container.querySelector('[data-role="user"]');
    expect(bubble).toBeTruthy();
    // The image thumbnail renders (name in alt — no text row repeats it) and
    // the doc chip shows its filename and sizing.
    expect(screen.getByAltText('shot.png')).toBeTruthy();
    expect(bubble!.textContent).toContain('report.pdf');
    expect(bubble!.textContent).toContain('PDF · 4.8 MB');
    // No text row: the only text is the chip copy itself.
    expect(screen.queryByText("Here's the shot")).toBeNull();
    expect(imgCount(container)).toBe(1); // image thumbnail, not the doc
    expect(screen.getByLabelText('Download report.pdf')).toBeTruthy();
  });

  it('renders a mixed entry with text, thumbnail, and doc chip (gallery J)', () => {
    render(<UserMessage m={{ id: 'm4', author: 'you', ts: '9:17', text: 'From the audit', attachments: [image, { name: 'dump.sql', mime: 'application/sql', size: 4300000, url: '/api/v1/files/k3' }] }} members={[]}/>);

    expect(screen.getByText('From the audit')).toBeTruthy();
    expect(screen.getByAltText('shot.png')).toBeTruthy();
    expect(screen.getByText('dump.sql')).toBeTruthy();
    expect(screen.getByText('SQL · 4.1 MB')).toBeTruthy();
  });

  it('renders exactly as before when the entry has no attachments', () => {
    render(<UserMessage m={{ id: 'm5', author: 'you', ts: '9:18', text: 'plain message' }} members={[]}/>);
    expect(screen.getByText('plain message')).toBeTruthy();
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.queryByLabelText(/Download/)).toBeNull();
  });
});

function imgCount(container: HTMLElement): number {
  return container.querySelectorAll('img').length;
}
