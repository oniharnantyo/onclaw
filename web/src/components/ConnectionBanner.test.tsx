import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ConnectionBanner } from './ConnectionBanner';
import { useConnectionStore } from '../store/connection';

describe('ConnectionBanner component', () => {
  beforeEach(() => {
    useConnectionStore.getState().reset();
    vi.restoreAllMocks();
  });

  it('renders nothing when connection is healthy (degraded: false)', () => {
    render(<ConnectionBanner />);
    expect(screen.queryByTestId('connection-banner')).toBeNull();
  });

  it('renders sticky banner and Retry button when connection is degraded', () => {
    useConnectionStore.getState().reportFailure();

    render(<ConnectionBanner />);

    const banner = screen.getByTestId('connection-banner');
    expect(banner).not.toBeNull();
    expect(banner.className).toContain('sticky');
    expect(banner.className).toContain('top-0');

    expect(screen.getByText('Connection lost. Reconnecting to OnClaw…')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Retry' })).not.toBeNull();
  });

  it('renders custom message when passed', () => {
    useConnectionStore.getState().reportFailure();

    render(<ConnectionBanner message="Offline mode active." />);

    expect(screen.getByText('Offline mode active.')).not.toBeNull();
  });

  it('calls onRetry callback when Retry button is clicked', async () => {
    useConnectionStore.getState().reportFailure();
    const onRetry = vi.fn().mockResolvedValue(undefined);

    render(<ConnectionBanner onRetry={onRetry} />);

    const retryBtn = screen.getByRole('button', { name: 'Retry' });
    fireEvent.click(retryBtn);

    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('disappears when connection is restored', () => {
    useConnectionStore.getState().reportFailure();

    const { rerender } = render(<ConnectionBanner />);
    expect(screen.getByTestId('connection-banner')).not.toBeNull();

    useConnectionStore.getState().reportSuccess();
    rerender(<ConnectionBanner />);

    expect(screen.queryByTestId('connection-banner')).toBeNull();
  });
});
