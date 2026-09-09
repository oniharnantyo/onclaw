/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { PromptBlockedNotice } from './PromptBlockedNotice';

// Blocked-prompt notice (integrate-agent-hooks D6): renders the transcript
// entry carrying the enforcing hook and reason in place of the assistant
// reply that never came.
describe('components/chat/PromptBlockedNotice', () => {
  it('renders the hook name and reason as a compact notice line', () => {
    render(
      <PromptBlockedNotice
        m={{ id: 'n1', author: 'notice', ts: '9:14 AM', notice: { hook: 'Compliance Gate', reason: 'prompts about payroll are routed to HR' } }}
      />
    );
    const notice = screen.getByTestId('prompt-blocked-notice');
    expect(notice.textContent).toContain('Blocked by hook');
    expect(notice.textContent).toContain('Compliance Gate');
    expect(notice.textContent).toContain('prompts about payroll are routed to HR');
    expect(notice.textContent).toContain('9:14 AM');
    // Message identity carries through for the transcript row hook-up.
    expect(notice.closest('[data-od-id="msg-n1"]')).not.toBeNull();
  });

  it('degrades without a hook name or reason (defensive payload)', () => {
    render(<PromptBlockedNotice m={{ id: 'n2', author: 'notice', notice: {} }} />);
    expect(screen.getByTestId('prompt-blocked-notice').textContent).toContain('Blocked by hook');
  });
});
