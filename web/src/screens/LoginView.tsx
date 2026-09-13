import React, { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { useAuth } from '../store/auth';
import { Icon } from '../components/ui/Icon';
import { ThemeCycleButton } from '../components/ui/ThemeCycleButton';

export function LoginView() {
  const navigate = useNavigate();
  const location = useLocation();
  const { login, isAuthenticated, isLoading } = useAuth();

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // If already authenticated, redirect to destination or home
  useEffect(() => {
    if (isAuthenticated) {
      const from = (location.state as any)?.from?.pathname || '/';
      navigate(from, { replace: true });
    }
  }, [isAuthenticated, navigate, location.state]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;

    setError(null);
    setSubmitting(true);

    try {
      await login(email.trim(), password);
      const from = (location.state as any)?.from?.pathname || '/';
      navigate(from, { replace: true });
    } catch {
      // Per spec: Uniform generic error message for every failure mode
      setError('Invalid email or password');
    } finally {
      setSubmitting(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-[100dvh] w-full items-center justify-center bg-bg">
        <div className="flex flex-col items-center gap-3">
          <div className="h-7 w-7 animate-spin rounded-full border-2 border-line border-t-accent" />
          <span className="text-[13px] text-muted font-medium">Checking session…</span>
        </div>
      </div>
    );
  }

  return (
    <div className="relative flex min-h-[100dvh] w-full items-center justify-center bg-bg p-4 antialiased">
      {/* Theme cycle control (change add-dark-theme): icon-only, pinned to the
          viewport's top-right corner, tooltip below. */}
      <div className="absolute right-4 top-4 z-10">
        <ThemeCycleButton variant="icon" testId="login-theme" />
      </div>
      <div className="od-pop w-full max-w-[380px] rounded-lg border border-line bg-surface p-8 shadow-[var(--elev-raised)]">
        {/* Brand Header */}
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="mb-3 flex h-11 w-11 items-center justify-center rounded-[12px] bg-accent text-[18px] font-bold text-accenton shadow-sm">
            O
          </div>
          <h1 className="text-[20px] font-semibold tracking-tight text-fg">Welcome to OnClaw</h1>
          <p className="mt-1 text-[13px] text-muted">Sign in with your workspace credentials</p>
        </div>

        {/* Error Alert */}
        {error && (
          <div
            data-testid="login-error"
            role="alert"
            className="mb-5 flex items-center gap-2 rounded-md border border-[color-mix(in_oklab,var(--danger)_30%,transparent)] bg-[color-mix(in_oklab,var(--danger)_8%,transparent)] px-3 py-2.5 text-[13px] text-danger"
          >
            <Icon name="alert" size={15} className="shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* Login Form */}
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <label htmlFor="email" className="mb-1.5 block text-[12px] font-medium text-fg2">
              Email address
            </label>
            <input
              id="email"
              type="email"
              required
              autoComplete="email"
              autoFocus
              value={email}
              onChange={(e) => {
                setEmail(e.target.value);
                if (error) setError(null);
              }}
              disabled={submitting}
              placeholder="you@company.com"
              className="w-full rounded-md border border-line bg-bg px-3 py-2 text-[13px] text-fg outline-none transition-all placeholder:text-muted focus:border-accent focus:ring-1 focus:ring-accent disabled:opacity-50"
            />
          </div>

          <div>
            <label htmlFor="password" className="mb-1.5 block text-[12px] font-medium text-fg2">
              Password
            </label>
            <input
              id="password"
              type="password"
              required
              autoComplete="current-password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
                if (error) setError(null);
              }}
              disabled={submitting}
              placeholder="••••••••"
              className="w-full rounded-md border border-line bg-bg px-3 py-2 text-[13px] text-fg outline-none transition-all placeholder:text-muted focus:border-accent focus:ring-1 focus:ring-accent disabled:opacity-50"
            />
          </div>

          <button
            type="submit"
            disabled={submitting || !email.trim() || !password}
            className="mt-2 flex h-10 w-full items-center justify-center gap-2 rounded-md bg-accent text-[13px] font-medium text-accenton transition-all hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? (
              <>
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-accenton/30 border-t-accenton" />
                <span>Signing in…</span>
              </>
            ) : (
              <span>Sign in</span>
            )}
          </button>
        </form>
      </div>
    </div>
  );
}
