import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import {
  TimezoneSelect,
  getTimezoneOffsetString,
  getSupportedTimezones,
  FALLBACK_TIMEZONES,
} from './TimezoneSelect';

describe('components/ui/TimezoneSelect', () => {
  it('calculates UTC offsets correctly', () => {
    const winterDate = new Date('2026-01-15T12:00:00Z');

    const utcOffset = getTimezoneOffsetString('UTC', winterDate);
    expect(utcOffset).toBe('UTC+00:00');

    const nyOffset = getTimezoneOffsetString('America/New_York', winterDate);
    expect(nyOffset).toBe('UTC-05:00');

    const tokyoOffset = getTimezoneOffsetString('Asia/Tokyo', winterDate);
    expect(tokyoOffset).toBe('UTC+09:00');

    const invalidOffset = getTimezoneOffsetString('Invalid/Zone', winterDate);
    expect(invalidOffset).toBe('UTC');
  });

  it('provides supported timezones or fallback list', () => {
    const list = getSupportedTimezones();
    expect(list.length).toBeGreaterThan(10);
    expect(list).toContain('UTC');
    expect(list).toContain('America/New_York');
    expect(list).toContain('Europe/London');
  });

  it('renders selected timezone with offset badge in trigger', () => {
    render(
      <TimezoneSelect
        value="America/New_York"
        onChange={vi.fn()}
      />
    );

    const trigger = screen.getByRole('combobox');
    expect(trigger.textContent).toContain('America/New_York');
    expect(trigger.textContent).toContain('UTC');
  });

  it('filters timezone list by city name, continent, or offset', () => {
    render(
      <TimezoneSelect
        value="UTC"
        onChange={vi.fn()}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('combobox-search-input');

    // Search by city
    fireEvent.change(searchInput, { target: { value: 'Tokyo' } });
    expect(screen.getByText('Asia/Tokyo')).not.toBeNull();

    // Search by continent
    fireEvent.change(searchInput, { target: { value: 'Europe/Berlin' } });
    expect(screen.getByText('Europe/Berlin')).not.toBeNull();

    // Search by offset
    fireEvent.change(searchInput, { target: { value: '+09:00' } });
    expect(screen.getByText('Asia/Tokyo')).not.toBeNull();
  });

  it('calls onChange when a timezone option is chosen', () => {
    const onChange = vi.fn();
    render(
      <TimezoneSelect
        value="UTC"
        onChange={onChange}
      />
    );

    fireEvent.click(screen.getByRole('combobox'));
    const searchInput = screen.getByTestId('combobox-search-input');
    fireEvent.change(searchInput, { target: { value: 'Europe/London' } });

    const londonOption = screen.getByTestId('combobox-option-Europe/London');
    fireEvent.mouseDown(londonOption);

    expect(onChange).toHaveBeenCalledWith('Europe/London');
  });

  it('includes custom value if not in standard list', () => {
    render(
      <TimezoneSelect
        value="Custom/Zone_Test"
        onChange={vi.fn()}
      />
    );

    expect(screen.getByRole('combobox').textContent).toContain('Custom/Zone_Test');
  });

  it('handles direct change event on underlying select for test/form compatibility', () => {
    const onChange = vi.fn();
    render(
      <div>
        <label htmlFor="ws-tz">Timezone</label>
        <TimezoneSelect
          id="ws-tz"
          value="America/Los_Angeles"
          onChange={onChange}
        />
      </div>
    );

    const select = screen.getByLabelText('Timezone');
    fireEvent.change(select, { target: { value: 'Europe/Berlin' } });

    expect(onChange).toHaveBeenCalledWith('Europe/Berlin');
  });
});
