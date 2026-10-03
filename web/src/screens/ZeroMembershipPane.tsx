import { useState } from "react";
import { ErrorState } from "../components/ErrorState";

export interface ZeroMembershipPaneProps {
  /** The signed-in user's email — quoted in the copyable request text. */
  email?: string;
  onToast?: (text: string, kind?: string) => void;
  onSignOut: () => void;
}

// The request text the copy affordance puts on the clipboard — there is no
// server-side request, so the user pastes it to their instance admin.
function workspaceRequestText(email?: string): string {
  return (
    'Hi — could you add me to an OnClaw workspace? My account is ' +
    (email || '(my work email)') +
    '.'
  );
}

// Zero-membership state (fix-role-permission-audit): a signed-in user with no
// workspace memberships lands here instead of the onboarding create flow —
// workspaces are created by an instance administrator. Replaces all
// workspace-scoped navigation.
export function ZeroMembershipPane({ email, onToast = () => {}, onSignOut }: ZeroMembershipPaneProps) {
  const [copied, setCopied] = useState(false);

  const copyRequest = () => {
    const text = workspaceRequestText(email);
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard
        .writeText(text)
        .then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 2500);
        })
        .catch(() => onToast('Clipboard blocked by the browser', 'danger'));
    } else {
      onToast('Clipboard API not available', 'danger');
    }
  };

  return (
    <main
      className="flex h-full w-full flex-1 items-center justify-center bg-surface p-6"
      data-od-id="zero-membership-pane"
      data-testid="zero-membership-pane"
    >
      <ErrorState
        variant="compact"
        icon="users"
        iconClassName="h-14 w-14 rounded-[16px] bg-[color-mix(in_oklab,var(--fg)_7%,transparent)] text-muted"
        title="No workspace yet"
        description="Your account is not a member of any workspace yet. Workspaces are created by this OnClaw instance's administrator — ask them to add you, then sign in again."
        primaryAction={{
          label: copied ? 'Request copied' : 'Copy a request for your admin',
          onClick: copyRequest,
          'data-testid': 'btn-copy-workspace-request',
        }}
        secondaryAction={{
          label: 'Sign out',
          onClick: onSignOut,
          'data-testid': 'btn-zero-membership-signout',
        }}
      />
    </main>
  );
}
