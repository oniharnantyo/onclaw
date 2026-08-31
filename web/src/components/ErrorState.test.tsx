import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ErrorState } from './ErrorState';

describe('ErrorState component', () => {
  it('renders full variant with 24px semibold title and 14px muted body', () => {
    render(
      <ErrorState
        variant="full"
        title="Something went wrong"
        description="An unexpected error occurred while loading this page."
      />
    );

    const titleEl = screen.getByRole('heading', { level: 2, name: 'Something went wrong' });
    expect(titleEl).not.toBeNull();
    expect(titleEl.className).toContain('text-[24px]');
    expect(titleEl.className).toContain('font-semibold');

    const descEl = screen.getByText('An unexpected error occurred while loading this page.');
    expect(descEl).not.toBeNull();
    expect(descEl.className).toContain('text-[14px]');
    expect(descEl.className).toContain('text-muted');
  });

  it('renders full variant illustration slot with w-64 sm:w-80 sizing and od-fade entry', () => {
    const { container } = render(
      <ErrorState
        variant="full"
        title="Page Not Found"
        illustration="/assets/not-found.svg"
      />
    );

    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    expect(img?.getAttribute('src')).toBe('/assets/not-found.svg');
    expect(img?.className).toContain('w-64');
    expect(img?.className).toContain('sm:w-80');
    expect(img?.className).toContain('od-fade');
  });

  it('renders compact variant with icon medallion and title', () => {
    const { container } = render(
      <ErrorState
        variant="compact"
        title="Failed to load items"
        description="Please try again."
        icon="alert"
      />
    );

    expect(screen.getByRole('heading', { level: 2, name: 'Failed to load items' })).not.toBeNull();
    expect(screen.getByText('Please try again.')).not.toBeNull();

    const medallion = container.querySelector('.rounded-full.bg-\\[color-mix\\(in_oklab\\,var\\(--danger\\)_15\\%\\,transparent\\)\\]');
    expect(medallion).not.toBeNull();
  });

  it('renders mono detail chip for explicit detail prop', () => {
    render(
      <ErrorState
        title="Crash error"
        detail="TypeError: Cannot read properties of undefined"
      />
    );

    const chip = screen.getByTestId('error-detail-chip');
    expect(chip).not.toBeNull();
    expect(chip.textContent).toBe('TypeError: Cannot read properties of undefined');
    expect(chip.className).toContain('font-mono');
    expect(chip.className).toContain('text-[12px]');
  });

  it('formats detail chip from status, code, and requestId', () => {
    render(
      <ErrorState
        title="Server Error"
        status={500}
        code="internal"
        requestId="req_1234567890abcdef"
      />
    );

    const chip = screen.getByTestId('error-detail-chip');
    expect(chip).not.toBeNull();
    expect(chip.textContent).toBe('500 · internal · req_1234567890abcdef');
  });

  it('renders primary and secondary action buttons with click handlers', () => {
    const onPrimary = vi.fn();
    const onSecondary = vi.fn();

    render(
      <ErrorState
        title="Lost Connection"
        primaryAction={{ label: 'Retry', onClick: onPrimary }}
        secondaryAction={{ label: 'Back to Dashboard', onClick: onSecondary }}
      />
    );

    const primaryBtn = screen.getByRole('button', { name: 'Retry' });
    const secondaryBtn = screen.getByRole('button', { name: 'Back to Dashboard' });

    expect(primaryBtn.className).toContain('bg-accent');
    expect(primaryBtn.className).toContain('text-[13px]');
    expect(secondaryBtn.className).toContain('border');
    expect(secondaryBtn.className).toContain('text-[13px]');

    fireEvent.click(primaryBtn);
    expect(onPrimary).toHaveBeenCalledTimes(1);

    fireEvent.click(secondaryBtn);
    expect(onSecondary).toHaveBeenCalledTimes(1);
  });

  it('renders action with href as anchor element', () => {
    render(
      <ErrorState
        title="Not Found"
        primaryAction={{ label: 'Go Home', href: '/' }}
      />
    );

    const link = screen.getByRole('link', { name: 'Go Home' });
    expect(link.getAttribute('href')).toBe('/');
  });
});
