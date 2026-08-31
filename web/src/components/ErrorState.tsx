import React from 'react';
import { cx } from '../lib/helpers';
import { Icon } from './ui/Icon';

export interface ActionConfig {
  label: React.ReactNode;
  onClick?: (e: React.MouseEvent<HTMLButtonElement | HTMLAnchorElement>) => void;
  to?: string;
  href?: string;
  disabled?: boolean;
  className?: string;
  'data-testid'?: string;
}

export interface ErrorStateProps {
  variant?: 'full' | 'compact';
  title: React.ReactNode;
  description?: React.ReactNode;
  body?: React.ReactNode;
  message?: React.ReactNode;
  illustration?: React.ReactNode | string;
  icon?: React.ReactNode | string;
  iconClassName?: string;
  detail?: React.ReactNode;
  status?: number | string;
  code?: string;
  requestId?: string;
  primaryAction?: ActionConfig;
  secondaryAction?: ActionConfig;
  actions?: React.ReactNode;
  className?: string;
  cardClassName?: string;
  children?: React.ReactNode;
}

function renderAction(action: ActionConfig, kind: 'primary' | 'secondary') {
  const baseClasses =
    kind === 'primary'
      ? 'inline-flex h-9 items-center justify-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] cursor-pointer disabled:opacity-50'
      : 'inline-flex h-9 items-center justify-center rounded-md border border-line bg-surface px-4 text-[13px] font-medium text-fg transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_5%,transparent)] cursor-pointer disabled:opacity-50';

  if (action.href) {
    return (
      <a
        key={String(action.label)}
        href={action.href}
        onClick={action.onClick}
        className={cx(baseClasses, action.className)}
        data-testid={action['data-testid']}
      >
        {action.label}
      </a>
    );
  }

  return (
    <button
      key={String(action.label)}
      type="button"
      onClick={action.onClick}
      disabled={action.disabled}
      className={cx(baseClasses, action.className)}
      data-testid={action['data-testid']}
    >
      {action.label}
    </button>
  );
}

export function ErrorState({
  variant = 'full',
  title,
  description,
  body,
  message,
  illustration,
  icon,
  iconClassName,
  detail,
  status,
  code,
  requestId,
  primaryAction,
  secondaryAction,
  actions,
  className,
  cardClassName,
  children,
}: ErrorStateProps) {
  const bodyContent = description ?? body ?? message;

  let detailContent: React.ReactNode = detail;
  if (!detailContent) {
    const parts: string[] = [];
    if (status !== undefined && status !== null && status !== '') parts.push(String(status));
    if (code) parts.push(code);
    if (requestId) parts.push(requestId);
    if (parts.length > 0) {
      detailContent = parts.join(' · ');
    }
  }

  const detailChip = detailContent ? (
    <div
      data-testid="error-detail-chip"
      className="inline-flex items-center gap-1.5 rounded-md border border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)] px-2.5 py-1 font-mono text-[12px] text-muted break-all max-w-full text-left"
    >
      {detailContent}
    </div>
  ) : null;

  const actionsRow = (primaryAction || secondaryAction || actions) ? (
    <div className="flex flex-wrap items-center justify-center gap-2 pt-2">
      {primaryAction && renderAction(primaryAction, 'primary')}
      {secondaryAction && renderAction(secondaryAction, 'secondary')}
      {actions}
    </div>
  ) : null;

  if (variant === 'compact') {
    return (
      <div className={cx('flex flex-col items-center justify-center p-6 text-center', className)}>
        <div className={cx('flex max-w-md flex-col items-center space-y-3', cardClassName)}>
          {icon && (
            <div
              className={cx(
                'mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-[color-mix(in_oklab,var(--danger)_15%,transparent)] text-danger',
                iconClassName
              )}
            >
              {typeof icon === 'string' ? <Icon name={icon} size={24} /> : icon}
            </div>
          )}
          <div className="space-y-1">
            <h2 className="text-[24px] font-semibold text-fg tracking-tight">{title}</h2>
            {bodyContent && (
              <p className="text-[14px] leading-5 text-muted max-w-sm mx-auto">{bodyContent}</p>
            )}
          </div>
          {detailChip}
          {actionsRow}
          {children}
        </div>
      </div>
    );
  }

  return (
    <div className={cx('fixed inset-0 z-50 bg-bg flex min-h-full flex-1 flex-col items-center justify-center p-6 text-center od-fade', className)}>
      <div className={cx('flex max-w-md flex-col items-center space-y-4', cardClassName)}>
        {illustration && (
          <div className="w-64 sm:w-80 max-w-full od-fade flex items-center justify-center select-none mb-2">
            {typeof illustration === 'string' ? (
              <img
                src={illustration}
                alt=""
                className="w-64 sm:w-80 max-w-full select-none object-contain od-fade"
              />
            ) : (
              illustration
            )}
          </div>
        )}
        <div className="space-y-1.5">
          <h2 className="text-[24px] font-semibold text-fg tracking-tight">{title}</h2>
          {bodyContent && (
            <p className="text-[14px] leading-5 text-muted max-w-sm mx-auto">{bodyContent}</p>
          )}
        </div>
        {detailChip}
        {actionsRow}
        {children}
      </div>
    </div>
  );
}

export default ErrorState;
