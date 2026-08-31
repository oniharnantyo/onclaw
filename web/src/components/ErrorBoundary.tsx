import React, { Component, type ErrorInfo, type ReactNode } from 'react';
import { ErrorState } from './ErrorState';
import serverErrorSvg from '../assets/server-error.svg';

export interface ErrorBoundaryProps {
  children: ReactNode;
  mode?: 'full' | 'shell';
  fallback?: ReactNode | ((error: Error, reset: () => void) => ReactNode);
  onError?: (error: Error, errorInfo: ErrorInfo) => void;
  title?: string;
  description?: string;
  illustration?: ReactNode | string;
  icon?: ReactNode | string;
}

export interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
  resetKey: number;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
      resetKey: 0,
    };
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    if (this.props.onError) {
      this.props.onError(error, errorInfo);
    }
  }

  reset = (): void => {
    this.setState((prev) => ({
      hasError: false,
      error: null,
      resetKey: prev.resetKey + 1,
    }));
  };

  handleReload = (): void => {
    if (typeof window !== 'undefined' && window.location) {
      window.location.reload();
    }
  };

  render(): ReactNode {
    const { hasError, error, resetKey } = this.state;
    const {
      mode = 'full',
      fallback,
      children,
      title,
      description,
      illustration = serverErrorSvg,
      icon,
    } = this.props;

    if (hasError && error) {
      if (fallback !== undefined) {
        return typeof fallback === 'function' ? fallback(error, this.reset) : fallback;
      }

      const errorMessage = error.message || String(error);

      if (mode === 'shell') {
        return (
          <div className="flex h-full w-full items-center justify-center p-6 bg-surface">
            <ErrorState
              variant="full"
              title={title || 'This view crashed'}
              description={description || 'An unexpected error occurred while rendering this view.'}
              illustration={illustration}
              icon={icon}
              detail={errorMessage}
              primaryAction={{
                label: 'Try again',
                onClick: this.reset,
              }}
              secondaryAction={{
                label: 'Reload app',
                onClick: this.handleReload,
              }}
            />
          </div>
        );
      }

      return (
        <div className="flex min-h-[100dvh] w-full items-center justify-center p-6 bg-bg">
          <ErrorState
            variant="full"
            title={title || 'Something went wrong'}
            description={description || 'An unexpected error caused the application to crash.'}
            illustration={illustration}
            icon={icon}
            detail={errorMessage}
            primaryAction={{
              label: 'Reload',
              onClick: this.handleReload,
            }}
          />
        </div>
      );
    }

    return <React.Fragment key={resetKey}>{children}</React.Fragment>;
  }
}

export default ErrorBoundary;
