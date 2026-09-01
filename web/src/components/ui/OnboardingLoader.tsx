import { useEffect, useState } from "react";

/** Rotating corporate-onboarding status lines shown while an agent deploys. */
const LOADING_MESSAGES = [
  "Finding the employee handbook…",
  "Asking HR for permission…",
  "Printing 47 onboarding documents…",
  "Looking for the missing signature…",
  "Sending a “Welcome!” email…",
  "Pretending to read the company policies…",
  "Finding a desk for the new hire…",
  "Assigning a buddy who actually knows things…",
  "Checking if the laptop is ready…",
  "Creating yet another Slack channel…",
  "Scheduling a “quick” intro meeting…",
  "Adding everyone to the calendar…",
  "Finding the right access permissions…",
  "Waiting for IT… 👀",
  "Asking “Can you see my screen?”",
  "Making the onboarding checklist look productive…",
  "Looking for the office Wi-Fi password…",
  "Explaining where the coffee machine is…",
  "Preparing the welcome swag…",
  "Practicing the welcome speech…",
  "Finding someone who knows the process…",
  "Making sure nobody forgot the new hire…",
  "Updating the spreadsheet nobody owns…",
  "Checking if HR has approved it…",
  "Sending another reminder to HR…",
  "Making onboarding slightly less painful…",
  "Turning paperwork into productivity…",
  "Making bureaucracy work for you…",
  "Negotiating with the onboarding gods…",
  "Convincing IT this is urgent…",
  "Making friends with the approval workflow…",
];

interface OnboardingLoaderProps {
  /** Primary line above the rotating status message. */
  label?: string;
  /** Message rotation interval in ms. */
  intervalMs?: number;
  /** How long to wait before the elapsed timer appears (ms). */
  showTimerAfterMs?: number;
}

/** Default rotation interval for the status messages. */
const DEFAULT_INTERVAL_MS = 5000;

/** Formats elapsed ms as "42s" or "1m 32s". */
export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

export function OnboardingLoader({
  label = "Onboarding agent…",
  intervalMs = DEFAULT_INTERVAL_MS,
  showTimerAfterMs = 30000,
}: OnboardingLoaderProps) {
  const [index, setIndex] = useState(() => Math.floor(Math.random() * LOADING_MESSAGES.length));
  const [elapsedMs, setElapsedMs] = useState(0);

  useEffect(() => {
    const start = Date.now();
    const messageTimer = setInterval(() => {
      setIndex((i) => (i + 1) % LOADING_MESSAGES.length);
    }, intervalMs);
    const tickTimer = setInterval(() => {
      setElapsedMs(Date.now() - start);
    }, 1000);
    return () => {
      clearInterval(messageTimer);
      clearInterval(tickTimer);
    };
  }, [intervalMs]);

  return (
    <div data-testid="onboarding-loader" className="flex flex-col items-center gap-3 py-12 text-center">
      <span className="h-6 w-6 animate-spin rounded-full border-2 border-line border-t-accent" />
      <p className="text-[13px] font-semibold text-fg">{label}</p>
      <p className="min-h-[16px] font-mono text-[12px] text-muted" data-testid="onboarding-loader-message">
        {LOADING_MESSAGES[index]}
      </p>
      {elapsedMs >= showTimerAfterMs && (
        <p className="font-mono text-[12px] text-muted" data-testid="onboarding-loader-timer">
          {formatElapsed(elapsedMs)}
        </p>
      )}
    </div>
  );
}
