import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { OnboardingLoader, formatElapsed } from './OnboardingLoader';

describe('ui/OnboardingLoader', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the label and a rotating message, without a timer before the threshold', () => {
    render(<OnboardingLoader label="Onboarding Radar…" showTimerAfterMs={30000} />);

    expect(screen.getByTestId('onboarding-loader')).not.toBeNull();
    expect(screen.getByText('Onboarding Radar…')).not.toBeNull();
    expect(screen.getByTestId('onboarding-loader-message').textContent).not.toBe('');
    expect(screen.queryByTestId('onboarding-loader-timer')).toBeNull();
  });

  it('shows the elapsed timer once the threshold passes and keeps counting', () => {
    render(<OnboardingLoader label="Onboarding Radar…" showTimerAfterMs={30000} />);

    act(() => {
      vi.advanceTimersByTime(31000);
    });
    expect(screen.getByTestId('onboarding-loader-timer').textContent).toBe('31s');

    act(() => {
      vi.advanceTimersByTime(61000);
    });
    expect(screen.getByTestId('onboarding-loader-timer').textContent).toBe('1m 32s');
  });

  it('formats elapsed time as seconds under a minute and m s above', () => {
    expect(formatElapsed(42000)).toBe('42s');
    expect(formatElapsed(92000)).toBe('1m 32s');
    expect(formatElapsed(0)).toBe('0s');
  });
});
