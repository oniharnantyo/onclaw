/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CompactionDivider, formatTokenSpan } from './CompactionDivider';

// Compaction divider (chat-compact-command, mockups C/D): one component for
// the just-happened live event and the hydrated history entry — the entry's
// summarySaved flag is the only difference.
describe('components/chat/CompactionDivider', () => {
  it('renders the live (just happened) variant with the summary-saved suffix', () => {
    render(
      <CompactionDivider
        m={{ id: 'c1', author: 'compaction', ts: '9:14 AM', text: '', summarySaved: true, compaction: { tokensBefore: 154000, tokensAfter: 9200 } }}
      />
    );
    const divider = document.querySelector('[data-role="compaction"]')!;
    expect(divider.textContent).toContain('Context compacted');
    expect(divider.textContent).toContain('154k → 9.2k tokens');
    expect(divider.textContent).toContain('summary saved to transcript');
    // Message identity carries through for the transcript row hook-up.
    expect(divider.matches('[data-od-id="msg-c1"]')).toBe(true);
  });

  it('renders the hydrated variant with the token counts only (mockup D)', () => {
    render(
      <CompactionDivider
        m={{ id: 'c2', author: 'compaction', ts: '', text: '', compaction: { tokensBefore: 154000, tokensAfter: 9200 } }}
      />
    );
    const divider = document.querySelector('[data-role="compaction"]')!;
    expect(divider.textContent).toContain('154k → 9.2k tokens');
    expect(divider.textContent).not.toContain('summary saved to transcript');
  });

  it('degrades defensively on a missing payload', () => {
    render(<CompactionDivider m={{ id: 'c3', author: 'compaction', text: '' }} />);
    expect(document.querySelector('[data-role="compaction"]')!.textContent).toContain('0 → 0 tokens');
  });
});

describe('formatTokenSpan', () => {
  it('formats the 154k → 9.2k mockup pair', () => {
    expect(formatTokenSpan(154000, 9200)).toBe('154k → 9.2k');
  });

  it('keeps values below 1000 raw', () => {
    expect(formatTokenSpan(940, 12)).toBe('940 → 12');
  });

  it('uses one decimal only when needed', () => {
    expect(formatTokenSpan(1000, 250000)).toBe('1k → 250k');
    expect(formatTokenSpan(9500, 12300)).toBe('9.5k → 12.3k');
  });
});
