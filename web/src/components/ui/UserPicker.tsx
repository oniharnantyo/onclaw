import React, { useState, useEffect, useMemo } from 'react';
import { Combobox, type ComboboxOption } from './Combobox';
import { Avatar } from './Avatar';
import { Icon } from './Icon';
import { cx } from '../../lib/helpers';
import { api, type ApiUser } from '../../lib/api';

export interface UserOption extends ComboboxOption {
  user: ApiUser;
}

export interface UserPickerProps {
  value?: string;
  onChange: (value: string, user?: ApiUser) => void;
  valueKey?: 'id' | 'email';
  users?: ApiUser[];
  disabledUserIds?: string[];
  excludeUserIds?: string[];
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

export function UserPicker({
  value,
  onChange,
  valueKey = 'id',
  users: propUsers,
  disabledUserIds = [],
  excludeUserIds = [],
  disabled = false,
  error = false,
  placeholder = 'Select a user…',
  searchPlaceholder = 'Search user by name or email…',
  className,
  id,
  name,
  'data-od-id': dataOdId,
  'data-testid': dataTestId,
  'aria-label': ariaLabel,
  'aria-labelledby': ariaLabelledBy,
}: UserPickerProps) {
  const [fetchedUsers, setFetchedUsers] = useState<ApiUser[]>([]);
  const [loading, setLoading] = useState(false);
  const [fetchError, setFetchError] = useState<string | null>(null);

  useEffect(() => {
    // If users are passed as prop, don't fetch
    if (propUsers !== undefined) return;

    let cancelled = false;
    setLoading(true);
    setFetchError(null);

    api.admin.users
      .list()
      .then((res) => {
        if (!cancelled) {
          setFetchedUsers(res.users || []);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setFetchError(err instanceof Error ? err.message : 'Failed to load users');
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [propUsers]);

  const userList = propUsers !== undefined ? propUsers : fetchedUsers;

  const options: UserOption[] = useMemo(() => {
    const excludedSet = new Set(excludeUserIds);
    const disabledSet = new Set(disabledUserIds);

    return userList
      .filter((u) => !excludedSet.has(u.id))
      .map((u) => {
        const val = valueKey === 'email' ? u.email : u.id;
        const isDisabled = Boolean(u.disabled_at || disabledSet.has(u.id));

        return {
          value: val,
          label: u.name || u.email,
          sublabel: u.email,
          disabled: isDisabled,
          user: u,
        };
      });
  }, [userList, excludeUserIds, disabledUserIds, valueKey]);

  const renderOption = (
    opt: UserOption,
    { selected }: { selected: boolean; active: boolean }
  ) => {
    const u = opt.user;
    const isDisabled = opt.disabled;

    return (
      <div className="flex w-full items-center gap-2.5">
        <Avatar
          name={u.name || u.email}
          src={u.avatar_url}
          size={24}
          kind="other"
        />
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex items-center gap-1.5 truncate">
            <span
              className={cx(
                'truncate text-[13px]',
                selected ? 'font-semibold text-fg' : 'font-medium text-fg',
                isDisabled && 'text-muted'
              )}
            >
              {u.name || u.email}
            </span>
            {u.is_superadmin && (
              <span className="rounded bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] px-1.5 py-0.2 font-mono text-[10px] font-medium text-accent">
                Superadmin
              </span>
            )}
            {isDisabled && (
              <span className="rounded bg-[color-mix(in_oklab,var(--danger)_12%,transparent)] px-1.5 py-0.2 font-mono text-[10px] font-medium text-danger">
                Disabled
              </span>
            )}
          </div>
          {u.name && u.email && (
            <span className="truncate text-[11px] text-muted">{u.email}</span>
          )}
        </div>
        {selected && (
          <Icon name="check" size={14} className="shrink-0 text-accent" />
        )}
      </div>
    );
  };

  const renderValue = (opt?: UserOption) => {
    if (!opt) return null;
    const u = opt.user;

    return (
      <div className="flex items-center gap-2 truncate">
        <Avatar
          name={u.name || u.email}
          src={u.avatar_url}
          size={20}
          kind="other"
        />
        <span className="truncate font-medium text-fg">{u.name || u.email}</span>
        {u.name && u.email && (
          <span className="truncate text-[12px] text-muted">({u.email})</span>
        )}
      </div>
    );
  };

  const filterOption = (opt: UserOption, query: string) => {
    const q = query.toLowerCase().trim();
    if (!q) return true;
    const u = opt.user;
    const nameMatch = u.name?.toLowerCase().includes(q);
    const emailMatch = u.email?.toLowerCase().includes(q);
    const idMatch = u.id?.toLowerCase().includes(q);
    return Boolean(nameMatch || emailMatch || idMatch);
  };

  const handleComboboxChange = (val: string, option?: UserOption) => {
    onChange(val, option?.user);
  };

  return (
    <Combobox<UserOption>
      options={options}
      value={value}
      onChange={handleComboboxChange}
      placeholder={loading ? 'Loading users…' : placeholder}
      searchPlaceholder={searchPlaceholder}
      disabled={disabled || loading}
      error={error || Boolean(fetchError)}
      renderOption={renderOption}
      renderValue={renderValue}
      filterOption={filterOption}
      emptyText={fetchError ? `Error: ${fetchError}` : 'No users found'}
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
