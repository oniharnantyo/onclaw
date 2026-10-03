import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { AuditTab } from './AuditTab';
import { api, type ApiAuditEntry } from '../../../lib/api';

const entry = (overrides: Partial<ApiAuditEntry> = {}): ApiAuditEntry => ({
  id: 'aud-1',
  workspace_id: 'acme',
  cluster_id: 'cl-deploy',
  skill_name: 'deploy-rollout',
  verdict: 'approved',
  diff: '+ canary then promote',
  reviewer: 'user_ada',
  created_at: '2026-09-28T10:00:00Z',
  ...overrides,
});

describe('screens/settings/curation/AuditTab', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('lists verdicts with reviewer, diff, and cluster — read-only', async () => {
    const listAudit = vi.spyOn(api.curation, 'listAudit').mockResolvedValue({ entries: [entry()] });

    render(<AuditTab wsSlug="acme" />);

    await waitFor(() => {
      expect(screen.getByTestId('audit-deploy-rollout')).not.toBeNull();
    });
    const row = screen.getByTestId('audit-deploy-rollout');
    expect(row.textContent).toContain('approved');
    expect(row.textContent).toContain('user_ada');
    expect(row.textContent).toContain('cluster · cl-deploy');
    expect(screen.getByTestId('audit-diff-deploy-rollout').textContent).toContain('canary then promote');
    // The default window is 30 days, sent as RFC 3339.
    expect(listAudit).toHaveBeenCalledWith('acme', expect.any(String));
    const since = listAudit.mock.calls[0][1] as string;
    expect(Number.isNaN(new Date(since).getTime())).toBe(false);
    const ageDays = (Date.now() - new Date(since).getTime()) / 86400000;
    expect(ageDays).toBeGreaterThan(29);
    expect(ageDays).toBeLessThan(31);
    // Read-only: no edit affordances.
    expect(screen.queryByTestId('btn-audit-edit-deploy-rollout')).toBeNull();
  });

  it('keeps a rejected proposal inspectable with its recorded reason', async () => {
    vi.spyOn(api.curation, 'listAudit').mockResolvedValue({
      entries: [entry({ id: 'aud-2', skill_name: 'legacy-importer', verdict: 'rejected', reason: 'Already covered by deploy-rollout' })],
    });

    render(<AuditTab wsSlug="acme" />);

    await waitFor(() => {
      expect(screen.getByTestId('audit-legacy-importer')).not.toBeNull();
    });
    expect(screen.getByTestId('audit-legacy-importer').textContent).toContain('rejected');
    expect(screen.getByTestId('audit-reason-legacy-importer').textContent).toContain(
      'Already covered by deploy-rollout'
    );
  });

  it('window picker changes the ?since= horizon', async () => {
    const listAudit = vi.spyOn(api.curation, 'listAudit').mockResolvedValue({ entries: [] });

    render(<AuditTab wsSlug="acme" />);
    await waitFor(() => {
      expect(listAudit).toHaveBeenCalledTimes(1);
    });
    fireEvent.click(screen.getByText('7 days'));

    await waitFor(() => {
      expect(listAudit).toHaveBeenCalledTimes(2);
    });
    const since = listAudit.mock.calls[1][1] as string;
    const ageDays = (Date.now() - new Date(since).getTime()) / 86400000;
    expect(ageDays).toBeGreaterThan(6);
    expect(ageDays).toBeLessThan(8);
  });

  it('names the empty state and survives a failed load', async () => {
    const listAudit = vi.spyOn(api.curation, 'listAudit');

    listAudit.mockResolvedValue({ entries: [] });
    const { unmount } = render(<AuditTab wsSlug="acme" />);
    await waitFor(() => {
      expect(screen.getByTestId('audit-empty')).not.toBeNull();
    });
    expect(screen.getByTestId('audit-empty').textContent).toContain('No decisions in this window');
    unmount();

    listAudit.mockRejectedValue(new Error('boom'));
    render(<AuditTab wsSlug="acme" />);
    await waitFor(() => {
      expect(screen.getByTestId('audit-error')).not.toBeNull();
    });
    expect(screen.getByTestId('btn-audit-retry')).not.toBeNull();
  });
});
