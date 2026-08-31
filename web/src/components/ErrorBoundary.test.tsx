import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import React, { useState } from 'react';
import { ErrorBoundary } from './ErrorBoundary';

function CrashingComponent({ shouldThrow, message = 'Crash test error' }: { shouldThrow: boolean; message?: string }) {
  if (shouldThrow) {
    throw new Error(message);
  }
  return <div data-testid="healthy-child">Healthy child content</div>;
}

function FlakyChild({ failFirst = false }: { failFirst?: boolean }) {
  const [shouldFail, setShouldFail] = useState(failFirst);

  if (shouldFail) {
    throw new Error('Flaky component failed');
  }

  return (
    <div>
      <div data-testid="recovered-child">Recovered successfully</div>
      <button type="button" onClick={() => setShouldFail(true)}>Trigger crash</button>
    </div>
  );
}

describe('ErrorBoundary component', () => {
  const originalConsoleError = console.error;

  beforeEach(() => {
    console.error = vi.fn(); // Suppress React error boundary console noise in test output
  });

  afterEach(() => {
    console.error = originalConsoleError;
    vi.restoreAllMocks();
  });

  it('renders children normally when no error occurs', () => {
    render(
      <ErrorBoundary>
        <CrashingComponent shouldThrow={false} />
      </ErrorBoundary>
    );

    expect(screen.getByTestId('healthy-child')).not.toBeNull();
  });

  it('catches render error and displays full-mode ErrorState with error message in mono chip', () => {
    render(
      <ErrorBoundary mode="full">
        <CrashingComponent shouldThrow={true} message="Critical rendering crash" />
      </ErrorBoundary>
    );

    expect(screen.getByRole('heading', { level: 2, name: 'Something went wrong' })).not.toBeNull();
    expect(screen.getByText('An unexpected error caused the application to crash.')).not.toBeNull();

    const chip = screen.getByTestId('error-detail-chip');
    expect(chip.textContent).toBe('Critical rendering crash');

    const reloadBtn = screen.getByRole('button', { name: 'Reload' });
    expect(reloadBtn).not.toBeNull();
  });

  it('invokes window.location.reload when Reload button is clicked in full mode', () => {
    const reloadMock = vi.fn();
    Object.defineProperty(window, 'location', {
      value: {
        ...window.location,
        reload: reloadMock,
      },
      writable: true,
      configurable: true,
    });

    render(
      <ErrorBoundary mode="full">
        <CrashingComponent shouldThrow={true} />
      </ErrorBoundary>
    );

    const reloadBtn = screen.getByRole('button', { name: 'Reload' });
    fireEvent.click(reloadBtn);

    expect(reloadMock).toHaveBeenCalledTimes(1);
  });

  it('renders in-shell error state in shell mode and resets subtree on Try again click', () => {
    let shouldCrash = true;
    function DynamicChild() {
      if (shouldCrash) {
        throw new Error('View failed to render');
      }
      return <div data-testid="dynamic-recovered">Recovered child</div>;
    }

    render(
      <ErrorBoundary mode="shell">
        <DynamicChild />
      </ErrorBoundary>
    );

    expect(screen.getByRole('heading', { level: 2, name: 'This view crashed' })).not.toBeNull();
    expect(screen.getByText('An unexpected error occurred while rendering this view.')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Reload app' })).not.toBeNull();

    const tryAgainBtn = screen.getByRole('button', { name: 'Try again' });
    expect(tryAgainBtn).not.toBeNull();

    // Change condition so child succeeds on retry
    shouldCrash = false;
    fireEvent.click(tryAgainBtn);

    expect(screen.getByTestId('dynamic-recovered')).not.toBeNull();
  });

  it('invokes onError callback when error is caught', () => {
    const onError = vi.fn();

    render(
      <ErrorBoundary onError={onError}>
        <CrashingComponent shouldThrow={true} message="Tracked crash" />
      </ErrorBoundary>
    );

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onError.mock.calls[0][0].message).toBe('Tracked crash');
  });

  it('renders custom fallback function with error and reset function', () => {
    let shouldCrash = true;
    function DynamicChild() {
      if (shouldCrash) {
        throw new Error('Custom crash');
      }
      return <div data-testid="custom-child-ok">OK</div>;
    }

    render(
      <ErrorBoundary
        fallback={(err, reset) => (
          <div>
            <span data-testid="custom-fallback-msg">{err.message}</span>
            <button type="button" onClick={reset}>Custom Reset</button>
          </div>
        )}
      >
        <DynamicChild />
      </ErrorBoundary>
    );

    expect(screen.getByTestId('custom-fallback-msg').textContent).toBe('Custom crash');

    shouldCrash = false;
    fireEvent.click(screen.getByRole('button', { name: 'Custom Reset' }));

    expect(screen.getByTestId('custom-child-ok')).not.toBeNull();
  });
});
