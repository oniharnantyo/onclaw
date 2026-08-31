import { useState } from "react";
import { Modal } from "../components/ui/Modal";
import { inputCls, labelCls } from "../components/ui/constants";
import { cx, uid, slugify } from "../lib/helpers";

export interface McpServerDialogProps {
  server?: any | null;
  existingServers?: any[];
  onClose: () => void;
  onSave: (server: any) => void;
  onToast: (text: string, kind?: string) => void;
}

export function McpServerDialog({
  server,
  existingServers = [],
  onClose,
  onSave,
  onToast,
}: McpServerDialogProps) {
  const isEdit = Boolean(server);
  const [name, setName] = useState(server?.name || '');
  const [transport, setTransport] = useState(server?.transport || '');

  const isValid = name.trim().length > 0 && transport.trim().length > 0;

  const handleSubmit = (e?: React.FormEvent) => {
    if (e) e.preventDefault();
    if (!isValid) return;

    const trimmedName = name.trim();
    const trimmedTransport = transport.trim();

    if (isEdit && server) {
      const updated = {
        ...server,
        name: trimmedName,
        transport: trimmedTransport,
      };
      onSave(updated);
      onToast(trimmedName + ' updated');
      onClose();
    } else {
      let slug = slugify(trimmedName) || uid('mcp');
      while (existingServers.some((x: any) => x.id === slug)) slug += '-2';
      const newServer = {
        id: slug,
        name: trimmedName,
        transport: trimmedTransport,
        auth: 'No credentials yet — configured on first launch',
        tools: 0,
        status: 'connected',
        sample: [],
      };
      onSave(newServer);
      onToast(trimmedName + ' added — tools sync on the first handshake');
      onClose();
    }
  };

  const submitBtnId = isEdit ? 'btn-mcp-save' : 'btn-mcp-add-confirm';

  return (
    <Modal
      title={isEdit ? `Edit ${server?.name}` : 'Add MCP server'}
      onClose={onClose}
      odId="modal-mcp-server"
      data-testid="modal-mcp-server"
      footer={
        <>
          <button
            type="button"
            onClick={onClose}
            data-od-id="btn-mcp-cancel"
            data-testid="btn-mcp-cancel"
            className="flex h-9 items-center rounded-md border border-line px-3.5 text-[13px] font-medium text-fg2 transition-colors hover:bg-[color-mix(in_oklab,var(--fg)_6%,transparent)] hover:text-fg"
          >
            Cancel
          </button>
          <button
            type="submit"
            form="mcp-server-form"
            data-od-id={submitBtnId}
            data-testid={submitBtnId}
            disabled={!isValid}
            className="flex h-9 items-center rounded-md bg-accent px-4 text-[13px] font-semibold text-accenton transition-colors hover:bg-[var(--accent-hover)] active:bg-[var(--accent-active)] disabled:opacity-40 disabled:hover:bg-accent"
          >
            {isEdit ? 'Save changes' : 'Add'}
          </button>
        </>
      }
    >
      <form id="mcp-server-form" onSubmit={handleSubmit} className="space-y-4 p-5">
        <div>
          <label className={labelCls} htmlFor="mcp-name">
            Server name
          </label>
          <input
            id="mcp-name"
            className={inputCls}
            placeholder="Server name — e.g. Sentry"
            value={name}
            aria-label="Server name"
            data-od-id="input-mcp-name"
            data-testid="input-mcp-name"
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
        </div>

        <div>
          <label className={labelCls} htmlFor="mcp-transport">
            Transport command
          </label>
          <input
            id="mcp-transport"
            className={cx(inputCls, 'font-mono text-[13px]')}
            placeholder="stdio · sentry-mcp serve"
            value={transport}
            aria-label="Transport command"
            data-od-id="input-mcp-transport"
            data-testid="input-mcp-transport"
            onChange={(e) => setTransport(e.target.value)}
          />
        </div>
      </form>
    </Modal>
  );
}
