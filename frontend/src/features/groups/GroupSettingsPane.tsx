/**
 * Group settings pane (add-agent-groups task 5.5, spec agent-groups "Group
 * surfaces in the chat menu"): two variants over one form. The dialog
 * variant (the rail's "+" create flow) renders the app's one dialog shell
 * (ModalShell) itself — title, body, and the Cancel + primary action pair in
 * the shell's footer slot — so the actions stay pinned above the edge while
 * long member lists scroll, and the state that gates them stays local. The
 * panel variant (round 7: the group conversation's right sidebar) renders
 * the same form IN PLACE inside the sidebar — a back + title header, the
 * scrollable body, and the actions as a bottom row — because a modal wider
 * than the sidebar clips against the window edge; the island ⇄ panel swap
 * follows the bar's Knowledge pattern.
 *
 * The edit mode edits the group's name, title, and instructions; lists the
 * members with add (an agent picker over the not-yet-member agents), remove,
 * and the crown lead control (design D5: exactly one lead — activating the
 * crown on a member row makes it the sole lead); and the delete-group flow
 * behind the codebase's confirm dialog (a modal in BOTH variants — a
 * destructive confirm stays centered even from a panel). Member and lead
 * mutations go straight through the groups bindings and refresh the store's
 * group state (the list, the open roster, the busy dots) through
 * refreshGroups — the same pass the live groups:changed subscription drives.
 * The create mode (spec "Group entity and membership", scenario "Create a
 * group with members") hosts the same fields empty, every agent as a
 * searchable initial-roster toggle, and Create through the store's
 * createGroup — which opens the new group's conversation. The backend makes
 * the FIRST member of memberAgentIDs the lead, so the create pane keeps the
 * roster as an ordered list and marks the prospective lead with the edit
 * pane's crown — clicking another selected member's crown reorders the
 * roster so that member leads. No delete in create mode (there is nothing to
 * delete yet). The dialog takes the shell's default width — the form's own
 * measure — like the schedule editor.
 */
import { useState } from "react";
import type { ChangeEvent, ReactElement } from "react";
import {
  ArrowLeft,
  Check,
  Crown,
  Plus,
  Search,
  Trash2,
  UserRoundPlus,
} from "lucide-react";

import type {
  Agent,
  AgentGroup,
  AgentGroupMember,
} from "../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { AgentAvatar, FormField, IconButton } from "../../components";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { ModalShell } from "../../components/ModalShell";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { SectionLabel } from "../../components/ui/SectionLabel";
import { Textarea } from "../../components/ui/Textarea";
import { cn } from "../../lib/cn";
import { client } from "../../lib/client";
import { toast } from "../../lib/toasts";
import { useChatStore } from "../chat/store";
import type { ExtendedChatStore } from "../chat/store";

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}

/**
 * The member list's search field (create pane's roster picker, edit pane's
 * add-member list): the rail's "Search sessions" input grammar — magnifier
 * icon in the leading edge, the kit's 13px bordered field — shared so both
 * pickers filter agents the same way.
 */
function MemberSearchInput({
  testId,
  value,
  onChange,
}: {
  testId: string;
  value: string;
  onChange: (value: string) => void;
}): ReactElement {
  return (
    <label className="relative block">
      <Search
        aria-hidden="true"
        className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted"
      />
      <input
        type="text"
        className="h-8 w-full rounded-md border border-line bg-surface ps-8 pe-2.5 text-[13px] text-fg2 placeholder:text-muted focus:border-accent"
        placeholder="Search agents"
        aria-label="Search agents"
        data-testid={testId}
        value={value}
        onChange={(e: ChangeEvent<HTMLInputElement>): void =>
          onChange(e.target.value)
        }
      />
    </label>
  );
}

export function GroupSettingsPane({
  group,
  onClosed,
  create = false,
  variant = "dialog",
}: {
  /** The row being edited; required in edit mode, unused in create mode. */
  group?: AgentGroup;
  /** Create mode: empty fields, the initial roster as a multi-select over
   * every agent, CreateGroup instead of UpdateGroup, no delete flow. */
  create?: boolean;
  /** Closes the hosting surface (the delete flow's exit, the create flow's
   * confirm exit, the panel variant's back affordance). */
  onClosed: () => void;
  /** "dialog" (default) wraps the form in ModalShell; "panel" renders it in
   * place for the right sidebar's island ⇄ panel swap (round 7). */
  variant?: "dialog" | "panel";
}): ReactElement {
  // SAFETY: the edit branch only mounts with the row being edited (the
  // right sidebar's group island passes the open group); the cast narrows
  // the optional prop for that branch alone. Create stays dialog-only —
  // it launches from the rail's "+" with no sidebar context.
  return create ? (
    <CreateGroupPane onClosed={onClosed} />
  ) : (
    <EditGroupPane
      group={group as AgentGroup}
      onClosed={onClosed}
      variant={variant}
    />
  );
}

/**
 * The create half (spec "Group entity and membership", scenario "Create a
 * group with members"): the same field grammar empty, every agent offered as
 * an initial-member toggle behind a name search (the edit pane's picker rows
 * read as a multi-select), and Create through the store's createGroup — which
 * refreshes the rail's list and opens the new group's conversation, so the
 * hosting dialog closes only on success. A refused create toasts the store
 * error and keeps the form (nothing is cleared). Name gates the button
 * (disabled until non-empty); title and instructions stay optional; with no
 * members picked the binding receives null. No delete — nothing exists yet —
 * and no separate lead control: the backend makes the FIRST member of
 * memberAgentIDs the lead, so the roster is an ordered list, the crown marks
 * the prospective lead, and clicking another selected member's crown reorders
 * the roster so that member leads. The actions end the pane in the Modal
 * footer's grammar (Cancel ghost left of the primary, right-aligned).
 */
function CreateGroupPane({ onClosed }: { onClosed: () => void }): ReactElement {
  const agents = useChatStore((s: ExtendedChatStore) => s.agents);
  const createGroup = useChatStore((s: ExtendedChatStore) => s.createGroup);

  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [instructions, setInstructions] = useState("");

  // The roster in submission order: the backend makes the first id the lead,
  // so order IS lead selection and the crown just reorders this list.
  const [roster, setRoster] = useState<string[]>([]);

  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);

  const leadID = roster[0] ?? "";

  const needle = query.trim().toLowerCase();

  const visible =
    needle === ""
      ? agents
      : agents.filter((a: Agent): boolean =>
          a.Name.toLowerCase().includes(needle)
        );

  const toggleMember = (agentID: string): void => {
    setRoster((prev: string[]): string[] =>
      prev.includes(agentID)
        ? prev.filter((id: string): boolean => id !== agentID)
        : [...prev, agentID]
    );
  };

  // Move the agent to the front of the roster — the marker follows the
  // order, and the submitted memberAgentIDs lead with the crowned member.
  const makeLead = (agentID: string): void => {
    setRoster((prev: string[]): string[] =>
      prev.includes(agentID)
        ? [agentID, ...prev.filter((id: string): boolean => id !== agentID)]
        : prev
    );
  };

  const create = (): void => {
    setCreating(true);

    void createGroup(
      name.trim(),
      title.trim(),
      instructions,
      roster.length > 0 ? roster : null
    )
      .then((created: AgentGroup | null): void => {
        if (created) {
          onClosed();

          return;
        }

        // A refused create keeps the form and announces the failure the way
        // the edit pane's mutations do (the store recorded the cause).
        toast(
          useChatStore.getState().error ?? "Failed to create group",
          "danger"
        );
      })
      .finally((): void => {
        setCreating(false);
      });
  };

  return (
    <ModalShell
      title="New group"
      testId="group-create-modal"
      onClose={onClosed}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            data-testid="btn-cancel-group"
            onClick={onClosed}
          >
            Cancel
          </Button>
          <Button
            variant="primary"
            size="sm"
            loading={creating}
            disabled={creating || name.trim() === ""}
            data-testid="btn-create-group"
            onClick={create}
          >
            Create
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-5" data-testid="group-create-pane">
        <section className="flex flex-col gap-3">
          <FormField id="group-name" label="Name">
            <Input
              id="group-name"
              value={name}
              placeholder="e.g. Research crew"
              data-testid="group-name-input"
              onChange={(e: React.ChangeEvent<HTMLInputElement>): void =>
                setName(e.target.value)
              }
            />
          </FormField>
          <FormField id="group-title" label="Title">
            <Input
              id="group-title"
              value={title}
              placeholder="Shown as the conversation's title; empty uses the name"
              data-testid="group-title-input"
              onChange={(e: React.ChangeEvent<HTMLInputElement>): void =>
                setTitle(e.target.value)
              }
            />
          </FormField>
          <FormField id="group-instructions" label="Instructions">
            <Textarea
              id="group-instructions"
              value={instructions}
              rows={4}
              placeholder="What this group works on — every member turn receives it"
              data-testid="group-instructions-input"
              onChange={(e: React.ChangeEvent<HTMLTextAreaElement>): void =>
                setInstructions(e.target.value)
              }
            />
          </FormField>
        </section>

        <section className="flex flex-col gap-2">
          <SectionLabel>Members</SectionLabel>
          <p className="text-[13px] text-muted">
            The first member added becomes the lead — the crown marks who.
          </p>
          {agents.length > 0 && (
            <MemberSearchInput
              testId="group-member-search"
              value={query}
              onChange={setQuery}
            />
          )}
          {visible.map((agent: Agent): ReactElement => {
            const picked = roster.includes(agent.ID);
            const isLead = picked && agent.ID === leadID;

            return (
              <div
                key={agent.ID}
                className={cn(
                  "flex items-center gap-2 rounded-md border px-2 py-1.5",
                  isLead
                    ? "border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
                    : picked
                      ? "border-line"
                      : "border-transparent"
                )}
                data-testid={`group-create-option-${agent.ID}`}
              >
                <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={22} />
                <span className="min-w-0 flex-1 truncate text-[13px] text-fg">
                  {agent.Name}
                </span>
                {/* The prospective-lead crown, the edit pane's SetLead grammar:
                    filled warn on the first-selected member, quiet on the rest,
                    and only rendered on selected rows — activating it reorders
                    the roster so that member is submitted first. */}
                {picked && (
                  <IconButton
                    label={
                      isLead
                        ? `Current lead — ${agent.Name}`
                        : `Make ${agent.Name} the lead`
                    }
                    size="sm"
                    testId={`group-create-lead-${agent.ID}`}
                    icon={
                      <Crown
                        aria-hidden="true"
                        className={cn(
                          "size-4",
                          isLead ? "text-warntext" : "text-muted"
                        )}
                      />
                    }
                    disabled={isLead || creating}
                    onClick={(): void => makeLead(agent.ID)}
                  />
                )}
                <IconButton
                  label={picked ? `Remove ${agent.Name}` : `Add ${agent.Name}`}
                  size="sm"
                  testId={`group-member-toggle-${agent.ID}`}
                  icon={
                    picked ? (
                      <Check
                        aria-hidden="true"
                        className="size-4 text-accent"
                      />
                    ) : (
                      <Plus aria-hidden="true" className="size-4 text-muted" />
                    )
                  }
                  disabled={creating}
                  onClick={(): void => toggleMember(agent.ID)}
                />
              </div>
            );
          })}
          {agents.length === 0 && (
            <p className="text-[13px] text-muted">
              No agents yet — create one first.
            </p>
          )}
          {agents.length > 0 && visible.length === 0 && (
            <p
              className="text-[13px] text-muted"
              data-testid="group-member-empty"
            >
              No agents match “{query.trim()}”.
            </p>
          )}
        </section>
      </div>
    </ModalShell>
  );
}

/**
 * The edit half (add-agent-groups task 5.5): the fields hydrate from the row,
 * the members list with the crown SetLead control (the current lead's row
 * also carries a persistent Lead badge — the marker, not just the hover
 * control), and the add-member list behind the same name search as the
 * create pane's picker. Save and Cancel end the pane in the Modal footer's
 * grammar; the delete flow stays in its own Danger section above it.
 */
function EditGroupPane({
  group,
  onClosed,
  variant = "dialog",
}: {
  group: AgentGroup;
  /** Closes the hosting surface (the delete flow's exit; the panel
   * variant's back affordance). */
  onClosed: () => void;
  /** "dialog" wraps the form in ModalShell; "panel" renders it in place for
   * the right sidebar's island ⇄ panel swap (round 7). */
  variant?: "dialog" | "panel";
}): ReactElement {
  const agents = useChatStore((s: ExtendedChatStore) => s.agents);
  const members = useChatStore((s: ExtendedChatStore) => s.groupMembers);
  const refreshGroups = useChatStore((s: ExtendedChatStore) => s.refreshGroups);

  // Editable fields hydrate from the open row; Save pushes them whole (the
  // UpdateGroup binding rewrites the three fields and nothing else).
  const [name, setName] = useState(group.Name);
  const [title, setTitle] = useState(group.Title);
  const [instructions, setInstructions] = useState(group.Instructions);
  const [nameError, setNameError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [addQuery, setAddQuery] = useState("");

  // Per-mutation in-flight markers so the rows' actions stay inert per
  // request (the agent pane's promotingSkill pattern).
  const [working, setWorking] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  const memberIDs = new Set(
    members.map((m: AgentGroupMember): string => m.AgentID)
  );

  const addable = agents.filter((a: Agent): boolean => !memberIDs.has(a.ID));

  const addNeedle = addQuery.trim().toLowerCase();

  const addableVisible =
    addNeedle === ""
      ? addable
      : addable.filter((a: Agent): boolean =>
          a.Name.toLowerCase().includes(addNeedle)
        );

  const agentOf = (agentID: string): Agent | undefined =>
    agents.find((a: Agent): boolean => a.ID === agentID);

  const run = (key: string, action: () => Promise<void>): Promise<void> => {
    setWorking(key);

    return action()
      .then((): Promise<void> => refreshGroups())
      .catch((err: Error): void => {
        toast(errorMessage(err, "The group update failed"), "danger");
      })
      .finally((): void => {
        setWorking(null);
      });
  };

  const save = (): void => {
    if (name.trim() === "") {
      setNameError("Name is required");

      return;
    }

    setNameError(null);
    setSaving(true);

    void client.groups
      .update(group.ID, name.trim(), title.trim(), instructions)
      .then((): Promise<void> => refreshGroups())
      .catch((err: Error): void => {
        toast(errorMessage(err, "Failed to save group"), "danger");
      })
      .finally((): void => {
        setSaving(false);
      });
  };

  const leadID = group.LeadAgentID ?? "";

  // The form body and the delete confirm are variant-independent — the two
  // wrappers (ModalShell vs the sidebar panel) share them, so the surfaces
  // cannot drift.
  const body = (
    <div className="flex flex-col gap-5" data-testid="group-settings-pane">
      <section className="flex flex-col gap-3">
        <FormField
          id="group-name"
          label="Name"
          invalid={nameError !== null}
          message={nameError ?? undefined}
        >
          <Input
            id="group-name"
            value={name}
            data-testid="group-name-input"
            onChange={(e: React.ChangeEvent<HTMLInputElement>): void =>
              setName(e.target.value)
            }
          />
        </FormField>
        <FormField id="group-title" label="Title">
          <Input
            id="group-title"
            value={title}
            placeholder="Shown as the conversation's title; empty uses the name"
            data-testid="group-title-input"
            onChange={(e: React.ChangeEvent<HTMLInputElement>): void =>
              setTitle(e.target.value)
            }
          />
        </FormField>
        <FormField id="group-instructions" label="Instructions">
          <Textarea
            id="group-instructions"
            value={instructions}
            rows={4}
            placeholder="What this group works on — every member turn receives it"
            data-testid="group-instructions-input"
            onChange={(e: React.ChangeEvent<HTMLTextAreaElement>): void =>
              setInstructions(e.target.value)
            }
          />
        </FormField>
      </section>

      <section className="flex flex-col gap-2">
        <SectionLabel>Members</SectionLabel>
        {members.length === 0 && (
          <p className="text-[13px] text-muted">
            No members yet — the first agent added becomes the lead.
          </p>
        )}
        {members.map((m: AgentGroupMember): ReactElement => {
          const agent = agentOf(m.AgentID);
          const isLead = m.AgentID === leadID;

          return (
            <div
              key={m.AgentID}
              className={cn(
                "flex items-center gap-2 rounded-md border px-2 py-1.5",
                isLead
                  ? "border-line bg-[color-mix(in_oklab,var(--fg)_4%,transparent)]"
                  : "border-transparent"
              )}
              data-testid={`group-member-${m.AgentID}`}
            >
              <AgentAvatar
                id={m.AgentID}
                avatar={agent?.Avatar ?? null}
                size={22}
              />
              <span className="min-w-0 flex-1 truncate text-[13px] text-fg">
                {agent?.Name || m.AgentID}
              </span>
              {/* The persistent lead marker: the badge names the lead at a
                    glance, while the crown beside it stays the reassign
                    control (spec "Lead control in settings"). */}
              {isLead && (
                <span
                  className="flex shrink-0 items-center gap-1 rounded-full bg-[color-mix(in_oklab,var(--warn)_14%,transparent)] px-1.5 py-0.5 text-[10px] font-semibold text-warntext"
                  data-testid={`group-lead-badge-${m.AgentID}`}
                >
                  <Crown aria-hidden="true" className="size-3" />
                  Lead
                </span>
              )}
              {/* The crown affordance (spec "Lead control in settings"):
                    filled for the current lead, quiet outline for the rest —
                    activating it makes that member the sole lead. */}
              <IconButton
                label={
                  isLead
                    ? "Current lead"
                    : `Make ${agent?.Name || m.AgentID} the lead`
                }
                size="sm"
                testId={`group-lead-${m.AgentID}`}
                icon={
                  <Crown
                    aria-hidden="true"
                    className={cn(
                      "size-4",
                      isLead ? "text-warntext" : "text-muted"
                    )}
                  />
                }
                disabled={isLead || working !== null}
                onClick={(): void => {
                  void run(`lead-${m.AgentID}`, (): Promise<void> =>
                    client.groups.setLead(group.ID, m.AgentID)
                  );
                }}
              />
              <IconButton
                label={`Remove ${agent?.Name || m.AgentID}`}
                size="sm"
                testId={`group-member-remove-${m.AgentID}`}
                icon={<Trash2 aria-hidden="true" className="size-4" />}
                disabled={working !== null}
                onClick={(): void => {
                  void run(`remove-${m.AgentID}`, (): Promise<void> =>
                    client.groups.removeMember(group.ID, m.AgentID)
                  );
                }}
              />
            </div>
          );
        })}
        {addable.length > 0 && (
          <div className="flex flex-col gap-1.5">
            <p className="flex items-center gap-1.5 text-[12px] font-medium text-fg2">
              <UserRoundPlus
                aria-hidden="true"
                className="size-3.5 text-muted"
              />
              Add a member
            </p>
            <MemberSearchInput
              testId="group-member-add-search"
              value={addQuery}
              onChange={setAddQuery}
            />
            {addableVisible.map((agent: Agent): ReactElement => (
              <div
                key={agent.ID}
                className="flex items-center gap-2 rounded-md px-2 py-1.5"
                data-testid={`group-add-option-${agent.ID}`}
              >
                <AgentAvatar id={agent.ID} avatar={agent.Avatar} size={22} />
                <span className="min-w-0 flex-1 truncate text-[13px] text-fg">
                  {agent.Name}
                </span>
                <IconButton
                  label={`Add ${agent.Name}`}
                  size="sm"
                  testId={`group-member-add-${agent.ID}`}
                  icon={<Plus aria-hidden="true" className="size-4" />}
                  disabled={working !== null}
                  onClick={(): void => {
                    void run(`add-${agent.ID}`, (): Promise<void> =>
                      client.groups.addMember(group.ID, agent.ID)
                    );
                  }}
                />
              </div>
            ))}
            {addableVisible.length === 0 && (
              <p className="text-[13px] text-muted">
                No available agents match “{addQuery.trim()}”.
              </p>
            )}
          </div>
        )}
      </section>

      <section className="flex flex-col gap-2 border-t border-line pt-4">
        <SectionLabel>Danger</SectionLabel>
        <Button
          variant="destructive"
          size="sm"
          data-testid="btn-delete-group"
          disabled={working !== null}
          onClick={(): void => setDeleting(true)}
        >
          Delete group
        </Button>
      </section>
    </div>
  );

  const confirm = deleting && (
    <ConfirmDialog
      title="Delete group?"
      testId="confirm-delete-group"
      confirmTestId="btn-confirm-delete-group"
      cancelTestId="btn-cancel-delete-group"
      onConfirm={(): void => {
        setDeleting(false);
        onClosed();
        void client.groups.delete(group.ID).then(
          (): Promise<void> => refreshGroups(),
          (err: Error): void => {
            toast(errorMessage(err, "Failed to delete group"), "danger");
          }
        );
      }}
      onClose={(): void => setDeleting(false)}
    >
      <p className="text-[13px] text-muted">
        “{group.Title || group.Name}” and its transcript, roster, and tasks will
        be removed. Member sessions survive. This cannot be undone.
      </p>
    </ConfirmDialog>
  );

  // Dialog variant (default): ModalShell owns the title + the Cancel/Save
  // footer; the confirm rides inside it.
  if (variant === "dialog") {
    return (
      <ModalShell
        title={`Group settings — ${group.Title || group.Name}`}
        testId="group-settings-modal"
        onClose={onClosed}
        actions={
          <>
            <Button
              variant="ghost"
              size="sm"
              data-testid="btn-cancel-group"
              onClick={onClosed}
            >
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              loading={saving}
              disabled={saving}
              data-testid="btn-save-group"
              onClick={save}
            >
              Save
            </Button>
          </>
        }
      >
        {body}
        {confirm}
      </ModalShell>
    );
  }

  // Panel variant (round 7): the form renders in place inside the right
  // sidebar — back + title header, scrollable body, actions as a bottom
  // row. The delete confirm stays a modal in both variants (a destructive
  // confirm never inlines).
  return (
    <div
      className="flex h-full min-h-0 w-full flex-1 flex-col"
      data-testid="group-settings-panel"
    >
      <div className="flex shrink-0 items-center gap-1.5 border-b border-line pb-2.5">
        <IconButton
          label="Back to group"
          size="sm"
          testId="btn-group-settings-back"
          icon={<ArrowLeft aria-hidden="true" className="size-4" />}
          onClick={onClosed}
        />
        <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-fg">
          Group settings
        </span>
      </div>
      <div className="od-scroll flex min-h-0 flex-1 flex-col overflow-y-auto pt-3">
        {body}
      </div>
      <div className="mt-3 flex shrink-0 items-center justify-end gap-2 border-t border-line pt-3">
        <Button
          variant="ghost"
          size="sm"
          data-testid="btn-cancel-group"
          onClick={onClosed}
        >
          Cancel
        </Button>
        <Button
          variant="primary"
          size="sm"
          loading={saving}
          disabled={saving}
          data-testid="btn-save-group"
          onClick={save}
        >
          Save
        </Button>
      </div>
      {confirm}
    </div>
  );
}
