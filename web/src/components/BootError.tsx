import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '../store/auth';
import { ErrorState } from './ErrorState';
import connectionSvg from '../assets/connection.svg';

export interface BootErrorProps {
  error?: string | null;
  onRetry?: () => void;
  onLoginInstead?: () => void;
}

export function BootError({ error, onRetry, onLoginInstead }: BootErrorProps = {}) {
  const storeBootError = useAuthStore((s) => s.bootError);
  const bootError = error !== undefined ? error : storeBootError;
  const navigate = useNavigate();

  const handleRetry = () => {
    if (onRetry) {
      onRetry();
      return;
    }
    useAuthStore.setState({ status: 'loading' });
    useAuthStore.getState().boot();
  };

  const handleLoginInstead = () => {
    if (onLoginInstead) {
      onLoginInstead();
      return;
    }
    useAuthStore.getState().clearSession();
    navigate('/login');
  };

  return (
    <div className="flex h-[100dvh] w-full items-center justify-center bg-bg p-6">
      <ErrorState
        variant="full"
        illustration={connectionSvg}
        title="Couldn't reach OnClaw"
        description="Your session could not be verified because the server is unreachable."
        detail={bootError ? <span data-testid="boot-error-detail">{bootError}</span> : undefined}
        primaryAction={{
          label: 'Retry',
          onClick: handleRetry,
        }}
        secondaryAction={{
          label: 'Log in instead',
          onClick: handleLoginInstead,
        }}
      />
    </div>
  );
}

export default BootError;

