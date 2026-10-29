/**
 * UploadModal (add-knowledge-corpus, "Knowledge management menu" / "Upload
 * with a scope choice"): one file, an explicit scope (Global, or one agent
 * — default Global), and an optional description. The client-side gates run
 * BEFORE the call so an unsupported kind or oversize file rejects with the
 * reason without a round trip; backend rejections (duplicate name in the
 * scope) land in the same inline error and the dialog stays open. Nothing
 * is listed as uploaded unless the call resolved.
 */
import { useRef, useState } from "react";
import type { ChangeEvent, ReactElement } from "react";

import type { Agent } from "../../../../bindings/github.com/oniharnantyo/buddies/internal/store/models";
import { Alert } from "../../../components/ui/Alert";
import { Button } from "../../../components/ui/Button";
import { Input } from "../../../components/ui/Input";
import { ModalShell } from "../../../components/ModalShell";
import { Select } from "../../../components/ui/Select";
import { client, type KnowledgeDocument } from "../../../lib/client";
import { errorMessage, fileToBase64, gateRejection } from "../helpers";

export interface UploadModalProps {
  /** The agents the scope picker offers. */
  agents: Agent[];
  onClose: () => void;
  /** Called once the upload resolved; the view refetches (the
   * knowledge:changed event also fires, this keeps the refresh
   * deterministic). */
  onUploaded: () => void;
}

export function UploadModal({
  agents,
  onClose,
  onUploaded,
}: UploadModalProps): ReactElement {
  const [file, setFile] = useState<File | null>(null);

  // "" = global scope; otherwise the target agent's ID (the Select's model).
  const [scopeAgentId, setScopeAgentId] = useState("");

  const [description, setDescription] = useState("");

  const [busy, setBusy] = useState(false);

  const [error, setError] = useState<string | null>(null);

  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const pickFile = (): void => {
    fileInputRef.current?.click();
  };

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>): void => {
    setFile(e.target.files?.[0] ?? null);
    setError(null);
  };

  const handleUpload = (): void => {
    if (!file || busy) return;

    const rejection = gateRejection(file.name, file.type, file.size);

    if (rejection !== null) {
      setError(rejection);

      return;
    }

    setBusy(true);
    setError(null);

    void fileToBase64(file)
      .then((base64: string): Promise<KnowledgeDocument> =>
        client.knowledge.upload(
          file.name,
          file.type,
          base64,
          scopeAgentId === "" ? "global" : "agent",
          scopeAgentId,
          description
        )
      )
      .then((): void => {
        onUploaded();
        onClose();
      })
      .catch((err: Error): void => {
        setError(errorMessage(err, "Upload failed"));
        setBusy(false);
      });
  };

  return (
    <ModalShell
      title="Upload document"
      testId="knowledge-upload-modal"
      onClose={onClose}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            data-testid="btn-cancel-upload"
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            data-testid="btn-confirm-upload"
            disabled={file === null || busy}
            onClick={handleUpload}
          >
            {busy ? "Uploading…" : "Upload"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <span className="text-[12px] font-medium text-fg2">File</span>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              data-testid="btn-choose-file"
              disabled={busy}
              onClick={pickFile}
            >
              Choose file…
            </Button>
            <span
              className="min-w-0 flex-1 truncate text-[13px] text-fg2"
              data-testid="knowledge-upload-filename"
            >
              {file ? `${file.name} · ${file.size} bytes` : "No file chosen"}
            </span>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            className="hidden"
            data-testid="knowledge-upload-file"
            onChange={handleFileChange}
          />
          <p className="text-[11px] text-muted">
            Text documents up to 1 MB; PDFs and OOXML documents up to 20 MB.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <label
            htmlFor="knowledge-upload-scope"
            className="text-[12px] font-medium text-fg2"
          >
            Scope
          </label>
          <Select
            id="knowledge-upload-scope"
            data-testid="knowledge-upload-scope"
            value={scopeAgentId}
            disabled={busy}
            onChange={(e: ChangeEvent<HTMLSelectElement>): void =>
              setScopeAgentId(e.target.value)
            }
          >
            <option value="">Global — every agent</option>
            {agents.map((a: Agent) => (
              <option key={a.ID} value={a.ID}>
                {a.Name}
              </option>
            ))}
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label
            htmlFor="knowledge-upload-description"
            className="text-[12px] font-medium text-fg2"
          >
            Description
          </label>
          <Input
            id="knowledge-upload-description"
            data-testid="knowledge-upload-description"
            value={description}
            disabled={busy}
            placeholder="Optional — helps discovery"
            onChange={(e: ChangeEvent<HTMLInputElement>): void =>
              setDescription(e.target.value)
            }
          />
        </div>

        {error && (
          <div role="alert">
            <Alert tone="danger" testId="knowledge-upload-error">
              {error}
            </Alert>
          </div>
        )}
      </div>
    </ModalShell>
  );
}
