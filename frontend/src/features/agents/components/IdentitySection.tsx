import { useEffect, useState } from "react";
import type { ReactElement } from "react";

import type { PromptDocuments } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/services/models";
import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { Button } from "../../../components/ui/Button";
import { Textarea } from "../../../components/ui/Textarea";
import { Alert } from "../../../components/ui/Alert";
import { MicroLabel } from "../../../components/ui/MicroLabel";
import { client } from "../../../lib/client";

/** One entry of the Identity file rail: rail label, workspace file name, caption. */
interface PromptDoc {
  key: "identity" | "soul" | "bootstrap";
  file: string;
  label: string;
  caption: string;
}

/** The three workspace documents, in turn-composition order (design D12). */
const PROMPT_DOCS: PromptDoc[] = [
  {
    key: "identity",
    file: "IDENTITY.md",
    label: "IDENTITY",
    caption: "who it is",
  },
  {
    key: "soul",
    file: "SOUL.md",
    label: "SOUL",
    caption: "how it sounds",
  },
  {
    key: "bootstrap",
    file: "BOOTSTRAP.md",
    label: "BOOTSTRAP",
    caption: "how it starts",
  },
];

/** The editable drafts for the three documents, keyed by rail entry. */
interface DocDrafts {
  identity: string;
  soul: string;
  bootstrap: string;
}

/** Which workspace file backs a rail entry (spec: save rewrites that file). */
function docExists(docs: PromptDocuments, key: PromptDoc["key"]): boolean {
  if (key === "identity") return docs.IdentityExists;

  if (key === "soul") return docs.SoulExists;

  return docs.BootstrapExists;
}

interface IdentitySectionProps {
  agent: Agent;
  /** Bubbles the re-read agent row after a regeneration (status may have changed). */
  onAgentRefreshed: (agent: Agent) => void;
}

/**
 * The config modal's Prompts panel (task 6.1): the file rail with the three
 * workspace documents in mono editors, the "What should change?"
 * regenerate form, and the failed banner whose retry path is that same form.
 * Slug and brief live next door on the Identity panel — both immutable.
 *
 * Documents load through getPromptDocuments when the agent changes and after
 * each regeneration; saving an editor rewrites that workspace file through
 * savePromptDocument (next turns pick it up with no restart); "Enhance
 * prompts" dispatches regeneratePrompts, then reloads the documents and the
 * agent row — the refreshed row bubbles up so the grid status follows.
 */
export function IdentitySection({
  agent,
  onAgentRefreshed,
}: IdentitySectionProps): ReactElement {
  const [selected, setSelected] = useState<PromptDoc["key"]>("identity");
  const [docs, setDocs] = useState<PromptDocuments | null>(null);

  const [drafts, setDrafts] = useState<DocDrafts>({
    identity: "",
    soul: "",
    bootstrap: "",
  });

  // Status lives here, not derived from the prop: the parent may hold a stale
  // row, and a regeneration re-reads it without reopening the modal.
  const [status, setStatus] = useState<string>(agent.PromptsStatus || "ready");
  const [statusError, setStatusError] = useState<string>(agent.PromptsError);
  const [instruction, setInstruction] = useState("");
  const [savingDoc, setSavingDoc] = useState<PromptDoc["key"] | null>(null);
  const [regenerating, setRegenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;

    void client.agents
      .getPromptDocuments(agent.ID)
      .then((loaded: PromptDocuments): void => {
        if (!mounted) return;
        setDocs(loaded);
        setDrafts({
          identity: loaded.Identity,
          soul: loaded.Soul,
          bootstrap: loaded.Bootstrap,
        });
      })
      .catch((err: Error): void => {
        if (mounted)
          setError(
            err instanceof Error
              ? err.message
              : "Failed to load prompt documents"
          );
      });

    return (): void => {
      mounted = false;
    };
  }, [agent.ID]);

  const busy = status === "generating" || regenerating;

  const selectedDoc: PromptDoc =
    PROMPT_DOCS.find((d: PromptDoc) => d.key === selected) ?? PROMPT_DOCS[0];

  const updateDraft = (key: PromptDoc["key"], value: string): void => {
    setDrafts((prev: DocDrafts) => {
      if (key === "identity") return { ...prev, identity: value };

      if (key === "soul") return { ...prev, soul: value };

      return { ...prev, bootstrap: value };
    });
  };

  const saveDocument = async (): Promise<void> => {
    if (savingDoc) return;
    setSavingDoc(selectedDoc.key);
    setError(null);

    try {
      await client.agents.savePromptDocument(
        agent.ID,
        selectedDoc.file,
        drafts[selectedDoc.key]
      );
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to save the document"
      );
    } finally {
      setSavingDoc(null);
    }
  };

  const regenerate = async (): Promise<void> => {
    if (busy) return;
    setRegenerating(true);
    setError(null);

    try {
      await client.agents.regeneratePrompts(agent.ID, instruction.trim());
      const loaded = await client.agents.getPromptDocuments(agent.ID);
      setDocs(loaded);
      setDrafts({
        identity: loaded.Identity,
        soul: loaded.Soul,
        bootstrap: loaded.Bootstrap,
      });
      const fresh = await client.agents.get(agent.ID);
      setStatus(fresh.PromptsStatus);
      setStatusError(fresh.PromptsError);
      onAgentRefreshed(fresh);
      setInstruction("");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Regeneration failed");
    } finally {
      setRegenerating(false);
    }
  };

  return (
    <section data-testid="config-section-prompts" className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <MicroLabel>Generated Prompts</MicroLabel>
        {status === "generating" && (
          <span
            data-testid="prompts-generating"
            className="text-xs font-medium text-accent"
            role="status"
          >
            Generating prompts...
          </span>
        )}
      </div>

      {status === "failed" && (
        <div role="alert" className="mt-3">
          <Alert tone="danger" testId="prompts-failed-banner">
            <p className="text-sm font-semibold">Prompt generation failed</p>
            {statusError && (
              <p data-testid="prompts-failed-reason" className="mt-0.5 text-sm">
                {statusError}
              </p>
            )}
            <p className="mt-1 text-xs text-muted">
              The agent still runs and stays editable — enhance the prompts
              below to retry.
            </p>
          </Alert>
        </div>
      )}

      <div className="mt-4 grid gap-3 md:grid-cols-[11rem_minmax(0,1fr)]">
        {/* File rail: one entry per workspace document (onclaw pill tabs). */}
        <div
          role="tablist"
          aria-label="Prompt documents"
          className="flex gap-1.5 overflow-x-auto md:flex-col md:overflow-visible"
        >
          {PROMPT_DOCS.map((doc: PromptDoc) => (
            <button
              key={doc.key}
              type="button"
              role="tab"
              aria-selected={selected === doc.key}
              data-testid={`prompt-file-${doc.key}`}
              onClick={() => setSelected(doc.key)}
              className={`flex shrink-0 items-center justify-between gap-2 rounded-md px-2.5 py-2 text-left transition-colors ${
                selected === doc.key
                  ? "bg-[color-mix(in_oklab,var(--accent)_14%,transparent)] text-fg"
                  : "text-muted hover:text-fg2"
              }`}
            >
              <span className="font-mono text-xs">{doc.label}</span>
              <span className="hidden text-[0.6875rem] text-muted md:inline">
                {doc.caption}
              </span>
            </button>
          ))}
        </div>

        {/* Editor for the selected document, reflecting the workspace file. */}
        <div className="min-w-0">
          <div className="flex items-center justify-between gap-2">
            <label
              htmlFor={`config-prompt-${selectedDoc.key}`}
              className="text-[12px] font-medium text-fg2"
            >
              <span className="font-mono">{selectedDoc.file}</span>
            </label>
            <Button
              size="sm"
              data-testid={`btn-save-prompt-${selectedDoc.key}`}
              disabled={busy || savingDoc !== null}
              onClick={() => void saveDocument()}
            >
              {savingDoc === selectedDoc.key ? "Saving…" : "Save document"}
            </Button>
          </div>
          <Textarea
            id={`config-prompt-${selectedDoc.key}`}
            data-testid={`input-prompt-${selectedDoc.key}`}
            className="font-mono text-sm leading-relaxed"
            rows={10}
            disabled={busy}
            value={drafts[selectedDoc.key]}
            placeholder={
              status === "generating"
                ? `Generating ${selectedDoc.file.toLowerCase()}…`
                : `${selectedDoc.file} content…`
            }
            onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
              updateDraft(selectedDoc.key, e.target.value)
            }
          />
          {docs && !docExists(docs, selectedDoc.key) && (
            <p
              data-testid={`prompt-not-generated-${selectedDoc.key}`}
              className="mt-1 text-xs text-muted"
            >
              Not generated yet — enhance the prompts to create this file.
            </p>
          )}
          {error && (
            <p
              role="alert"
              data-testid="prompt-doc-error"
              className="mt-1 text-xs text-dangertext"
            >
              {error}
            </p>
          )}
        </div>
      </div>

      {/* Regenerate form: doubles as the failed state's retry path. */}
      <div className="mt-4 border-t border-linesoft pt-3">
        <div className="flex flex-col gap-1.5">
          <label
            htmlFor="config-regen-instruction"
            className="text-[12px] font-medium text-fg2"
          >
            What should change?
          </label>
          <Textarea
            id="config-regen-instruction"
            data-testid="input-regen-instruction"
            rows={2}
            maxLength={2000}
            disabled={busy}
            value={instruction}
            placeholder="e.g. make the tone sharper and add incident-triage duties — leave empty to enhance as-is"
            onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
              setInstruction(e.target.value)
            }
          />
          <p className="text-xs text-muted">
            The current documents always ride along — the model enhances them in
            place and keeps a {"<name>.bak"} of each previous version.
          </p>
        </div>
        <div className="mt-2 flex items-center justify-end gap-3">
          {regenerating && (
            <span
              data-testid="regen-progress"
              className="text-[13px] text-muted"
              role="status"
            >
              Enhancing prompts… this can take a couple of minutes.
            </span>
          )}
          <Button
            size="sm"
            data-testid="btn-enhance-prompts"
            disabled={busy}
            onClick={() => void regenerate()}
          >
            {regenerating ? "Enhancing…" : "Enhance prompts"}
          </Button>
        </div>
      </div>
    </section>
  );
}
