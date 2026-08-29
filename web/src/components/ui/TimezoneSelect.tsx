import React, { useMemo } from 'react';
import { Combobox, type ComboboxOption } from './Combobox';
import { cx } from '../../lib/helpers';
import { Icon } from './Icon';

export const FALLBACK_TIMEZONES = [
  'Africa/Cairo',
  'Africa/Casablanca',
  'Africa/Johannesburg',
  'Africa/Lagos',
  'Africa/Nairobi',
  'America/Anchorage',
  'America/Argentina/Buenos_Aires',
  'America/Bogota',
  'America/Chicago',
  'America/Denver',
  'America/Halifax',
  'America/Los_Angeles',
  'America/Mexico_City',
  'America/New_York',
  'America/Phoenix',
  'America/Santiago',
  'America/Sao_Paulo',
  'America/Toronto',
  'America/Vancouver',
  'Asia/Bangkok',
  'Asia/Dubai',
  'Asia/Hong_Kong',
  'Asia/Jakarta',
  'Asia/Jerusalem',
  'Asia/Kolkata',
  'Asia/Manila',
  'Asia/Seoul',
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Taipei',
  'Asia/Tokyo',
  'Atlantic/Reykjavik',
  'Australia/Adelaide',
  'Australia/Brisbane',
  'Australia/Melbourne',
  'Australia/Perth',
  'Australia/Sydney',
  'Europe/Amsterdam',
  'Europe/Athens',
  'Europe/Berlin',
  'Europe/Brussels',
  'Europe/Dublin',
  'Europe/Helsinki',
  'Europe/Istanbul',
  'Europe/Lisbon',
  'Europe/London',
  'Europe/Madrid',
  'Europe/Paris',
  'Europe/Rome',
  'Europe/Stockholm',
  'Europe/Vienna',
  'Europe/Warsaw',
  'Europe/Zurich',
  'Pacific/Auckland',
  'Pacific/Guam',
  'Pacific/Honolulu',
  'Pacific/Noumea',
  'Pacific/Pago_Pago',
  'Pacific/Tongatapu',
  'UTC',
];

export function getTimezoneOffsetString(tz: string, date: Date = new Date()): string {
  try {
    const utcDateStr = date.toLocaleString('en-US', { timeZone: 'UTC' });
    const tzDateStr = date.toLocaleString('en-US', { timeZone: tz });
    const utcTime = new Date(utcDateStr).getTime();
    const tzTime = new Date(tzDateStr).getTime();
    const diffMinutes = Math.round((tzTime - utcTime) / 60000);
    const sign = diffMinutes >= 0 ? '+' : '-';
    const absMin = Math.abs(diffMinutes);
    const hours = String(Math.floor(absMin / 60)).padStart(2, '0');
    const mins = String(absMin % 60).padStart(2, '0');
    return `UTC${sign}${hours}:${mins}`;
  } catch {
    return 'UTC';
  }
}

export function getSupportedTimezones(): string[] {
  try {
    if (typeof Intl !== 'undefined' && typeof (Intl as any).supportedValuesOf === 'function') {
      const list = (Intl as any).supportedValuesOf('timeZone') as string[];
      if (Array.isArray(list) && list.length > 0) {
        if (!list.includes('UTC')) {
          return ['UTC', ...list];
        }
        return list;
      }
    }
  } catch {
    // fallback to static list
  }
  return FALLBACK_TIMEZONES;
}

export interface TimezoneOption extends ComboboxOption {
  timezone: string;
  offset: string;
}

export interface TimezoneSelectProps {
  value?: string;
  onChange: (timezone: string) => void;
  disabled?: boolean;
  error?: boolean | string;
  placeholder?: string;
  searchPlaceholder?: string;
  className?: string;
  id?: string;
  name?: string;
  'data-od-id'?: string;
  'data-testid'?: string;
  'aria-label'?: string;
  'aria-labelledby'?: string;
}

export function TimezoneSelect({
  value = 'UTC',
  onChange,
  disabled = false,
  error = false,
  placeholder = 'Select timezone…',
  searchPlaceholder = 'Search timezone (e.g. London, UTC+8)…',
  className,
  id,
  name,
  'data-od-id': dataOdId,
  'data-testid': dataTestId,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledBy,
}: TimezoneSelectProps) {
  const options: TimezoneOption[] = useMemo(() => {
    const rawList = getSupportedTimezones();
    const zones = new Set(rawList);
    if (value && !zones.has(value)) {
      zones.add(value);
    }

    return Array.from(zones).map((tz) => {
      const offset = getTimezoneOffsetString(tz);
      return {
        value: tz,
        label: tz,
        sublabel: offset,
        timezone: tz,
        offset,
      };
    });
  }, [value]);

  const renderOption = (
    opt: TimezoneOption,
    { selected }: { selected: boolean; active: boolean }
  ) => {
    return (
      <div className="flex w-full items-center justify-between gap-2">
        <span
          className={cx(
            'truncate',
            selected ? 'font-medium text-fg' : 'text-fg2'
          )}
        >
          {opt.timezone}
        </span>
        <div className="flex shrink-0 items-center gap-1.5">
          <span className="rounded bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] px-1.5 py-0.5 font-mono text-[11px] text-muted">
            {opt.offset}
          </span>
          {selected && (
            <Icon name="check" size={14} className="text-accent" />
          )}
        </div>
      </div>
    );
  };

  const renderValue = (opt?: TimezoneOption) => {
    if (!opt) return null;
    return (
      <div className="flex items-center gap-2 truncate">
        <span className="truncate text-fg">{opt.timezone}</span>
        <span className="font-mono text-[11px] text-muted">({opt.offset})</span>
      </div>
    );
  };

  const filterOption = (opt: TimezoneOption, query: string) => {
    const q = query.toLowerCase().trim();
    if (!q) return true;
    const tzLower = opt.timezone.toLowerCase();
    const cleanTz = tzLower.replace(/_/g, ' ');
    const offsetLower = opt.offset.toLowerCase();
    const cleanOffset = offsetLower.replace('utc', '');
    return (
      tzLower.includes(q) ||
      cleanTz.includes(q) ||
      offsetLower.includes(q) ||
      cleanOffset.includes(q)
    );
  };

  return (
    <Combobox<TimezoneOption>
      options={options}
      value={value}
      onChange={(tz) => onChange(tz)}
      placeholder={placeholder}
      searchPlaceholder={searchPlaceholder}
      disabled={disabled}
      error={error}
      renderOption={renderOption}
      renderValue={renderValue}
      filterOption={filterOption}
      id={id}
      name={name}
      className={className}
      data-od-id={dataOdId}
      data-testid={dataTestId}
      aria-label={ariaLabel}
      aria-labelledby={ariaLabelledBy}
    />
  );
}
