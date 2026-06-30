import React, { useState } from 'react';
import { Terminal, LockKey, Eye, EyeSlash } from '@phosphor-icons/react';

interface LoginProps {
  onLoginSuccess: () => void;
  showToast: (msg: string, type?: 'success' | 'error') => void;
}

export default function Login({ onLoginSuccess, showToast }: LoginProps) {
  const [password, setPassword] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password.trim() || isLoading) return;

    setIsLoading(true);
    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      });
      if (res.ok) {
        onLoginSuccess();
        showToast('Successfully logged in');
      } else {
        const errData = await res.json();
        showToast(errData.error || 'Invalid credentials', 'error');
      }
    } catch {
      showToast('Network error during login', 'error');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="login-container">
      <form
        className="login-card"
        onSubmit={handleLogin}
        aria-label="Sign in form"
        noValidate
      >
        {/* Logo */}
        <div className="login-logo">
          <div className="login-logo-mark" aria-hidden="true">
            <Terminal size={24} weight="bold" />
          </div>
          <div className="login-logo-text">ONCLAW</div>
        </div>

        <div className="login-title">Sign in to your console</div>

        {/* Passphrase field */}
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label" htmlFor="login-passphrase">
            <span style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
              <LockKey size={13} weight="bold" aria-hidden />
              Passphrase
            </span>
          </label>
          <div className="input-group">
            <input
              id="login-passphrase"
              type={showPassword ? 'text' : 'password'}
              className="form-input"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Enter your passphrase"
              autoFocus
              autoComplete="current-password"
              required
              disabled={isLoading}
              style={{ paddingRight: '40px' }}
            />
            <button
              type="button"
              onClick={() => setShowPassword((s) => !s)}
              aria-label={showPassword ? 'Hide passphrase' : 'Show passphrase'}
              style={{
                position: 'absolute',
                right: '10px',
                background: 'none',
                border: 'none',
                cursor: 'pointer',
                color: 'var(--text-muted)',
                display: 'flex',
                alignItems: 'center',
                padding: '0',
                transition: 'color var(--t-fast)',
              }}
              onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text)')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-muted)')}
            >
              {showPassword ? <EyeSlash size={16} /> : <Eye size={16} />}
            </button>
          </div>
        </div>

        <button
          id="login-submit-btn"
          type="submit"
          className="btn btn-primary"
          style={{ width: '100%', marginTop: '4px' }}
          disabled={isLoading || !password.trim()}
        >
          {isLoading ? (
            <>
              <span
                style={{
                  width: '14px',
                  height: '14px',
                  border: '2px solid rgba(10,15,30,0.3)',
                  borderTopColor: '#0a0f1e',
                  borderRadius: '50%',
                  animation: 'spin 0.75s linear infinite',
                  display: 'inline-block',
                  flexShrink: 0,
                }}
                aria-hidden
              />
              Authenticating…
            </>
          ) : (
            'Unlock Console'
          )}
        </button>
      </form>
    </div>
  );
}
