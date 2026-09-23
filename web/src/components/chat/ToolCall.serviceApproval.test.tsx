/**
 * @vitest-environment jsdom
 */
// Service-run write escalation card (add-integration-authority 3.2): the
// webhook-triggered run paused at a write-tier connection tool and the
// transcript renders the approval card in place of the shell approval card.
// Covers the four states (pending / approved / denied / expired), the
// actionability contract (buttons only when the parent passes the — permission
// gated — approval callback), and the copy that names the agent, the service,
// the tool, and what approving means. Also guards the branch: a command-only
// approval still renders the shell card.
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { ToolCall } from './ToolCall';

vi.mock('../../lib/api', () => ({
  api: {
    onUnauthorized: vi.fn(),
  },
}));

// The card object shape the runtime/livechat emitters push (tool rides only on
// service-run write escalations).
const serviceApproval = (overrides: Record<string, any> = {}) => ({
  args: '',
  ms: 0,
  approval: {
    interruptId: 'int-1',
    command: '',
    resolved: false,
    sessionId: 'sess_1',
    tool: {
      name: 'github.merge_pull_request',
      service: 'github',
      service_name: 'GitHub',
      connection_id: 'conn-gh',
      tier: 'write',
    },
    ...overrides,
  },
});

const renderCard = (t: any, approval?: (interruptId: string, approved: boolean) => Promise<void>) =>
  render(<ToolCall t={t} approval={approval} agentName="Atlas" />);

describe('components/chat/ToolCall — service-run write escalation card', () => {
  it('renders the pending card with approve/deny actions for permitted viewers', async () => {
    const approval = vi.fn().mockResolvedValue(undefined);
    const { container } = renderCard(serviceApproval(), approval);

    const card = container.querySelector('[data-testid="connection-approval-int-1"]');
    expect(card).not.toBeNull();
    // Copy names the agent, the tool, and the service.
    expect(card!.textContent).toContain('Atlas');
    expect(card!.textContent).toContain('github.merge_pull_request');
    expect(card!.textContent).toContain('GitHub');
    // Approving is explained in real terms.
    expect(card!.textContent).toContain('Approving lets the run make this one call');
    expect(card!.textContent).toContain('Denying ends the turn without it');

    fireEvent.click(screenbutton(card!, 'Approve'));
    await waitFor(() => {
      expect(approval).toHaveBeenCalledWith('int-1', true);
    });
  });

  it('denies from the same card and shows the working state while in flight', async () => {
    let resolveApproval: (v?: any) => void = () => {};
    const approval = vi.fn().mockImplementation(
      () => new Promise((resolve) => { resolveApproval = resolve; })
    );
    const { container } = renderCard(serviceApproval(), approval);

    fireEvent.click(screenbutton(container, 'Deny'));
    // The button flips to its working label while the call is in flight.
    expect(screenbutton(container, 'Denying…')).not.toBeNull();
    resolveApproval();
    await waitFor(() => {
      expect(approval).toHaveBeenCalledWith('int-1', false);
    });
  });

  it('renders the pending state read-only when no approval callback is passed (Member view)', () => {
    const { container } = renderCard(serviceApproval());

    expect(screenbutton(container, 'Approve')).toBeNull();
    expect(screenbutton(container, 'Deny')).toBeNull();
    const readonly = container.querySelector('[data-testid="connection-approval-readonly-int-1"]');
    expect(readonly).not.toBeNull();
    expect(readonly!.textContent).toContain('Waiting for an Owner or Admin');
  });

  it('shows the approved and denied outcomes read-only', () => {
    const approved = renderCard(serviceApproval({ approved: true }));
    expect(
      approved.container.querySelector('[data-testid="connection-approval-status-int-1"]')!.textContent
    ).toBe('approved');
    expect(screenbutton(approved.container, 'Approve')).toBeNull();

    const denied = renderCard(serviceApproval({ approved: false }));
    expect(
      denied.container.querySelector('[data-testid="connection-approval-status-int-1"]')!.textContent
    ).toBe('denied');
    expect(screenbutton(denied.container, 'Deny')).toBeNull();
  });

  it('shows the expired state read-only, from the wire flag or a passed deadline', () => {
    const flagged = renderCard(serviceApproval({ expired: true }));
    expect(
      flagged.container.querySelector('[data-testid="connection-approval-status-int-1"]')!.textContent
    ).toBe('expired');
    expect(flagged.container.textContent).toContain('This request expired');
    expect(screenbutton(flagged.container, 'Approve')).toBeNull();

    const lapsed = renderCard(
      serviceApproval({ expires_at: new Date(Date.now() - 60_000).toISOString() })
    );
    expect(
      lapsed.container.querySelector('[data-testid="connection-approval-status-int-1"]')!.textContent
    ).toBe('expired');
    expect(screenbutton(lapsed.container, 'Approve')).toBeNull();

    // A live deadline keeps the card actionable.
    const live = renderCard(
      serviceApproval({ expires_at: new Date(Date.now() + 60_000).toISOString() }),
      vi.fn().mockResolvedValue(undefined)
    );
    expect(screenbutton(live.container, 'Approve')).not.toBeNull();
  });

  it('keeps the shell approval card for command-only payloads', () => {
    const shell = {
      args: '',
      ms: 0,
      approval: { interruptId: 'int-2', command: 'rm -rf /tmp/scratch', resolved: false },
    };
    const { container } = render(<ToolCall t={shell} approval={vi.fn()} agentName="Atlas" />);
    // The shell card keeps its own mono header and command body.
    expect(container.querySelector('[data-od-id="approval-int-2"]')).not.toBeNull();
    expect(container.textContent).toContain('execute');
    expect(container.textContent).toContain('rm -rf /tmp/scratch');
    // No service-escalation card leaked in.
    expect(container.querySelector('[data-testid="connection-approval-int-2"]')).toBeNull();
  });
});

/** Button lookup by accessible name within a container. */
function screenbutton(container: Element, name: string): HTMLButtonElement | null {
  const buttons = Array.from(container.querySelectorAll('button'));
  return buttons.find((b) => b.textContent?.trim() === name) || null;
}
