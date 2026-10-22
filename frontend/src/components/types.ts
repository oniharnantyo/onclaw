import type { ReactNode } from "react";
import type { AvatarFullConfig } from "react-nice-avatar";

export interface ModalShellProps {
  /** Heading of the dialog box. */
  title: string;
  /** Secondary line under the title. */
  description?: string;
  /** Identifies the dialog in tests. */
  testId?: string;
  /** Widens the box for form-heavy dialogs. */
  wide?: boolean;
  onClose: () => void;
  /** Dialog body, rendered between the title and the action row. */
  children?: ReactNode;
  /** Footer buttons; rendered in the dialog's action row. */
  actions?: ReactNode;
}

export interface ConfirmDialogProps {
  title: string;
  /** Body copy and any warning block, rendered above the action row. */
  children?: ReactNode;
  testId?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Ink of the confirm button: brand accent, or destructive red. */
  tone?: "primary" | "error";
  confirmDisabled?: boolean;
  confirmTestId?: string;
  cancelTestId?: string;
  onConfirm: () => void;
  onClose: () => void;
}

export interface FormFieldProps {
  /** Id of the control the label points at. */
  id: string;
  /** Label content — a string, or a node carrying muted/accent suffixes. */
  label: ReactNode;
  /** The form control itself. */
  children: ReactNode;
  /** Inline error or hint under the control. */
  message?: string;
  /** Renders `message` in the error tone. */
  invalid?: boolean;
  /** Overrides the generated `field-error-<id>` test id. */
  messageTestId?: string;
}

export interface EmptyStateProps {
  title: string;
  description?: string;
  /** Call to action, e.g. a "Create your first agent" button. */
  action?: ReactNode;
  /** `boxed` for a whole surface, `inline` for an empty list inside a pane. */
  variant?: "boxed" | "inline";
  testId?: string;
}

export interface LoadingHintProps {
  label?: string;
  testId?: string;
}

export interface IconButtonProps {
  /** Accessible name and native tooltip text. */
  label: string;
  onClick: () => void;
  disabled?: boolean;
  icon: ReactNode;
  size?: "sm" | "md";
  testId?: string;
  className?: string;
}

export interface PageHeaderProps {
  title: string;
  /** Right-aligned affordance, usually the primary action. */
  action?: ReactNode;
  /** Applied to the header row (margins, spacing). */
  className?: string;
}

export interface PaneHeaderProps {
  /** Heading of the pane; omitted when the settings page's own h2 already
   * names the pane and the header carries only description/actions. */
  title?: string;
  /** Muted line under the title row. */
  description?: string;
  /** Right-aligned affordances, usually the primary action. */
  actions?: ReactNode;
  /** Overrides the generated `settings-pane-header` test id. */
  testId?: string;
}

export interface AgentAvatarProps {
  /** Agent id; the same id always yields the same generated avatar. */
  id: string;
  /**
   * The agent's stored procedural-avatar config (store.Agent.Avatar). When
   * present it wins over the id-derived face; absent or empty falls back to
   * the derivation.
   */
  avatar?: AvatarFullConfig | null;
  size?: number;
}

export interface SettingsListProps {
  children: ReactNode;
  testId?: string;
}

export interface SettingsRowProps {
  title: string;
  /** Brand tile or icon rendered before the title. */
  lead?: ReactNode;
  /** Status chips rendered after the title. */
  badges?: ReactNode;
  /** Secondary line under the title. */
  detail?: ReactNode;
  /** Extra block under the label column (e.g. an inline test result). */
  extra?: ReactNode;
  /** Trailing controls. */
  actions?: ReactNode;
  testId?: string;
}
