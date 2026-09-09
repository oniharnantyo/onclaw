import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { HookDialog } from './HookDialog';
import { api, type ApiHook } from '../lib/api';
import { toolCatalog } from '../lib/toolCatalog';

// jsdom lacks the browser APIs CodeMirror needs — stub the lazy loader with a
// promise that never resolves, so every test exercises the textarea fallback,
// which drives the same onSyntaxError seam the CodeMirror linter drives in a
// real browser (and keeps async state updates out of act).
vi.mock('./scriptEditorLoader', () => ({
  loadScriptEditor: () => new Promise(() => {}),
}));

// Format is exercised against a stub so no real parser ships into the test.
// The stub preserves the wrapper the way real Prettier does — its output for
// a wrapped expression (`(function (input) { … });`) passes the tolerant
// wrapper shape check, so the post-format save goes through.
vi.mock('prettier/standalone', () => ({
  format: vi.fn(async (source: string) => `(function (input) { ${source.trim()} });`),
}));
vi.mock('prettier/plugins/babel', () => ({ parsers: {} }));
vi.mock('prettier/plugins/estree', () => ({ printers: {} }));

// A valid stored script under the wrapped contract: `config.script` IS the
// complete `(function(input){ … })` expression — what the editor shows is
// what is stored and what runs.
const WRAPPED_SCRIPT = '(function(input){\n  return { decision: "block" };\n})';

describe('modals/HookDialog — script handler (D22/D23)', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api.mcp, 'list').mockResolvedValue({ servers: [] });
    vi.spyOn(api.providers, 'list').mockResolvedValue({ providers: [] });
    vi.spyOn(toolCatalog, 'ensure').mockResolvedValue(undefined);
  });

  async function renderDialog(props: Partial<Parameters<typeof HookDialog>[0]> = {}) {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(<HookDialog wsSlug="acme" onClose={onClose} onSave={onSave} {...props} />);
    // Flush the mount-time catalog/provider fetches inside act.
    await act(async () => {});
    return { onSave, onClose };
  }

  function fillScriptHook(script: string) {
    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'js-gate' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'script' } });
    fireEvent.change(screen.getByTestId('input-hook-script'), { target: { value: script } });
  }

  it('offers the script handler and swaps in the editor, helper, and 5s timeout', async () => {
    await renderDialog();

    expect(screen.getByRole('option', { name: 'script — JavaScript, in-process' })).not.toBeNull();

    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'script' } });

    expect(screen.getByTestId('input-hook-script')).not.toBeNull();
    expect(screen.getByTestId('hook-script-helper').textContent).toContain('Runs inside OnClaw');
    expect(screen.getByTestId('hook-script-helper').textContent).toContain('wrapper is the harness structure');
    expect(screen.getByTestId('button-hook-script-format')).not.toBeNull();
    expect((screen.getByTestId('input-hook-timeout') as HTMLInputElement).value).toBe('5000');
  });

  it('blocks submit while the script is empty, with the required error', async () => {
    const { onSave } = await renderDialog();

    fillScriptHook('   ');
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('hook-script-error').textContent).toContain('Script is required');
  });

  it('blocks submit while the editor reports a syntax error and disables Format', async () => {
    const { onSave } = await renderDialog();

    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'script' } });
    fireEvent.change(screen.getByTestId('input-hook-script'), {
      target: { value: '(function(input){\nif (input.tool {\n})' },
    });
    fireEvent.change(screen.getByTestId('input-hook-name'), { target: { value: 'js-gate' } });

    expect((screen.getByTestId('button-hook-script-format') as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByTestId('btn-hook-save'));

    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('hook-script-error').textContent).toMatch(/syntax error/i);
  });

  it('blocks submit when the wrapper is stripped — a shape error, not a syntax error', async () => {
    const { onSave } = await renderDialog();

    fillScriptHook('return { decision: "block" };');

    // The body alone still compiles as a function body, so this is not a
    // parse problem and Format stays available.
    expect((screen.getByTestId('button-hook-script-format') as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByTestId('btn-hook-save'));

    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByTestId('hook-script-error').textContent).toBe(
      'Keep the (function(input){ … }) wrapper — edit only the body inside'
    );
  });

  it('seeds the wrapped starter template on switch and never overwrites an existing script', async () => {
    await renderDialog();

    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'script' } });
    const seeded = (screen.getByTestId('input-hook-script') as HTMLTextAreaElement).value;
    expect(seeded).toContain('decision: "allow"');
    expect(seeded).toMatch(/^\s*\(function\s*\(\s*input\s*\)\s*\{/);

    // Author edits survive a handler round-trip — the seed only fills an empty buffer.
    fireEvent.change(screen.getByTestId('input-hook-script'), { target: { value: 'return;' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'http' } });
    fireEvent.change(screen.getByTestId('select-hook-handler'), { target: { value: 'script' } });
    expect((screen.getByTestId('input-hook-script') as HTMLTextAreaElement).value).toBe('return;');
  });

  it('wraps a legacy body-only stored script for display, so a re-save upgrades it', async () => {
    await renderDialog({
      hook: {
        id: 'hook-1',
        name: 'js-gate',
        event: 'pre_tool_use',
        matcher: 'web.fetch',
        handler_type: 'script',
        config: { script: 'return { decision: "block" };' },
        timeout_ms: 5000,
        on_failure: 'allow',
        enabled: true,
        position: 0,
        status: 'ok',
      } as ApiHook,
    });

    expect((screen.getByTestId('input-hook-script') as HTMLTextAreaElement).value).toBe(
      '(function(input){\nreturn { decision: "block" };\n})'
    );
  });

  it('builds the script payload with the wrapped source verbatim and the default 5s timeout', async () => {
    const { onSave } = await renderDialog();

    fillScriptHook(WRAPPED_SCRIPT);
    fireEvent.click(screen.getByTestId('btn-hook-save'));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledTimes(1);
    });
    const payload = onSave.mock.calls[0][0];
    expect(payload.handler_type).toBe('script');
    expect(payload.config).toEqual({ script: WRAPPED_SCRIPT });
    expect(payload.timeout_ms).toBe(5000);
  });

  it('formats the buffer in place through the lazy Prettier import', async () => {
    const { onSave } = await renderDialog();

    fillScriptHook(WRAPPED_SCRIPT);
    fireEvent.click(screen.getByTestId('button-hook-script-format'));

    const formatted = `(function (input) { ${WRAPPED_SCRIPT} });`;
    await waitFor(() => {
      expect((screen.getByTestId('input-hook-script') as HTMLTextAreaElement).value).toBe(formatted);
    });

    fireEvent.click(screen.getByTestId('btn-hook-save'));
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledTimes(1);
    });
    expect(onSave.mock.calls[0][0].config).toEqual({ script: formatted });
  });

  it('renders captured console output in the test panel result', async () => {
    vi.spyOn(api.hooks, 'test').mockResolvedValue({
      decision: 'block',
      reason: 'destructive command',
      duration_ms: 3,
      console_lines: ['preTool: rm -rf', 'blocked'],
    });

    await renderDialog();
    fillScriptHook(WRAPPED_SCRIPT);
    fireEvent.click(screen.getByTestId('btn-hook-test-toggle'));
    fireEvent.click(screen.getByTestId('btn-hook-test-run'));

    await waitFor(() => {
      expect(screen.getByTestId('hook-test-console')).not.toBeNull();
    });
    expect(screen.getByTestId('hook-test-console').textContent).toContain('preTool: rm -rf');
    expect(screen.getByTestId('hook-test-console').textContent).toContain('blocked');
  });
});
